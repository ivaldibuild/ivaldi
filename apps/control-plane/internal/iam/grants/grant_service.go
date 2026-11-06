// Package grants grants and removes roles on resources: /v1/grants.
package grants

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type GrantServiceOption func(*options)

type options struct {
	grants     iam.GrantRepository
	resources  iam.ResourceRepository
	principals iam.PrincipalRepository
	orgs       iam.OrganizationRepository
	teams      iam.TeamGroupSource
	authz      authorization.Authorizer
	writer     change.Writer
}

func WithGrantRepo(r iam.GrantRepository) GrantServiceOption {
	return func(o *options) { o.grants = r }
}

// WithResourceRepo checks where a role may be granted, and finds the
// resources above one.
func WithResourceRepo(r iam.ResourceRepository) GrantServiceOption {
	return func(o *options) { o.resources = r }
}

// WithPrincipalRepo checks that a grantee principal is active.
func WithPrincipalRepo(r iam.PrincipalRepository) GrantServiceOption {
	return func(o *options) { o.principals = r }
}

// WithOrganizationRepo locks the Organization while an owner grant is
// removed, so two owners removing each other cannot leave it with none.
func WithOrganizationRepo(r iam.OrganizationRepository) GrantServiceOption {
	return func(o *options) { o.orgs = r }
}

// WithTeamGroupSource finds a Team's group, to grant roles to Teams.
func WithTeamGroupSource(s iam.TeamGroupSource) GrantServiceOption {
	return func(o *options) { o.teams = s }
}

func WithAuthorizer(a authorization.Authorizer) GrantServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor, WithOutboxRecorder, and WithAuditRecorder commit each
// change with its events and audit record.
func WithTransactor(tx transaction.Transactor) GrantServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) GrantServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) GrantServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewGrantService returns the GrantService, which asks the authorizer
// before every operation. Every option is required.
func NewGrantService(opts ...GrantServiceOption) (iam.GrantService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"GrantRepo", o.grants != nil},
		{"ResourceRepo", o.resources != nil},
		{"PrincipalRepo", o.principals != nil},
		{"OrganizationRepo", o.orgs != nil},
		{"TeamGroupSource", o.teams != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("grant service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("grant service: %w", err)
	}
	return &authorized{next: &service{options: o}, grants: o.grants, authz: o.authz}, nil
}

// service carries out grant operations once they are authorized, and
// commits each change with its events and audit record.
type service struct {
	options
}

// granteeKinds are the principals a role may be granted to. Agent Sessions
// are limited by their scopes instead, and workloads act for the platform.
var granteeKinds = map[iam.PrincipalKind]bool{
	iam.PrincipalUser:           true,
	iam.PrincipalAgent:          true,
	iam.PrincipalServiceAccount: true,
}

func (s *service) ListGrants(ctx context.Context, _ identity.Actor, q iam.ListResourceGrantsQuery) (*pagination.Result[iam.GrantEntry], error) {
	ids := []string{q.ResourceID}
	if q.IncludeAncestors {
		var err error
		if ids, err = s.resources.ResourceAncestors(ctx, q.OrganizationID, q.ResourceID); err != nil {
			return nil, err
		}
	} else if _, err := s.resources.GetResource(ctx, q.OrganizationID, q.ResourceID); err != nil {
		return nil, err
	}
	return s.grants.ListGrants(ctx, iam.ListGrantsQuery{
		OrganizationID: q.OrganizationID,
		ResourceIDs:    ids,
		IncludeExpired: q.IncludeExpired,
		At:             clock.Now(),
		Page:           q.Page,
	})
}

func (s *service) CreateGrant(ctx context.Context, actor identity.Actor, in iam.CreateGrantInput) (*iam.GrantEntry, error) {
	role, err := iam.BuiltInRole(in.Role)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, id := range []string{in.PrincipalID, in.GroupID, in.TeamID} {
		if id != "" {
			n++
		}
	}
	if n != 1 {
		return nil, iam.NewErrInvalidInput("name exactly one grantee: a principal, a group, or a Team")
	}

	var entry *iam.GrantEntry
	err = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		res, err := s.resources.GetResource(ctx, in.OrganizationID, in.ResourceID)
		if errors.Is(err, iam.ErrNotFound) {
			return iam.NewErrInvalidInput(fmt.Sprintf("resource %s is not one of the Organization's", in.ResourceID))
		}
		if err != nil {
			return err
		}
		if res.Archived {
			return fmt.Errorf("%w: resource %s is archived", iam.ErrStatus, in.ResourceID)
		}
		if err := role.CheckGrantableAt(res.Type); err != nil {
			return err
		}

		props := iam.NewGrantProps{
			OrganizationID:       in.OrganizationID,
			RoleID:               role.ID(),
			ResourceID:           in.ResourceID,
			GrantedByPrincipalID: actor.PrincipalID,
			ExpiresAt:            in.ExpiresAt,
		}
		switch {
		case in.PrincipalID != "":
			if err := s.checkGrantee(ctx, in.OrganizationID, in.PrincipalID); err != nil {
				return err
			}
			props.PrincipalID = &in.PrincipalID
		case in.TeamID != "":
			groupID, err := s.teams.TeamGroupID(ctx, in.OrganizationID, in.TeamID)
			if errors.Is(err, iam.ErrNotFound) {
				return iam.NewErrInvalidInput(fmt.Sprintf("team %s is not one of the Organization's", in.TeamID))
			}
			if err != nil {
				return err
			}
			props.GroupID = &groupID
		default:
			props.GroupID = &in.GroupID
		}

		g, err := iam.NewGrant(props, nil)
		if err != nil {
			return err
		}
		if err := s.grants.Write(ctx, g); err != nil {
			if errors.Is(err, iam.ErrNotFound) {
				return iam.NewErrInvalidInput(fmt.Sprintf("group %s is not one of the Organization's", in.GroupID))
			}
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateGrant, g.ID(), g); err != nil {
			return err
		}
		if err := s.writer.AuditRecord(ctx, audit.Event{
			Type: iam.EventGrantCreated, OrganizationID: in.OrganizationID, ResourceID: g.ID(),
			Actor: actor, After: g.Snapshot(),
		}); err != nil {
			return err
		}
		entry, err = s.entry(ctx, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// checkGrantee refuses a principal that is not one of the Organization's
// active users, Agents, or service accounts.
func (s *service) checkGrantee(ctx context.Context, organizationID, principalID string) error {
	p, err := s.principals.Get(ctx, organizationID, principalID)
	if errors.Is(err, iam.ErrNotFound) {
		return iam.NewErrInvalidInput(fmt.Sprintf("principal %s is not one of the Organization's", principalID))
	}
	if err != nil {
		return err
	}
	if !granteeKinds[p.Kind] {
		return iam.NewErrInvalidInput(fmt.Sprintf("a role cannot be granted to a %s", p.Kind))
	}
	if p.Status != iam.PrincipalActive {
		return fmt.Errorf("%w: principal %s is %s", iam.ErrStatus, principalID, p.Status)
	}
	return nil
}

// entry reads the new grant back with its grantee named.
func (s *service) entry(ctx context.Context, g *iam.Grant) (*iam.GrantEntry, error) {
	page, err := s.grants.ListGrants(ctx, iam.ListGrantsQuery{
		OrganizationID: g.OrganizationID(),
		ResourceIDs:    []string{g.ResourceID()},
		GrantID:        g.ID(),
		IncludeExpired: true,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Items) != 1 {
		return nil, fmt.Errorf("read grant %s back: found %d", g.ID(), len(page.Items))
	}
	return &page.Items[0], nil
}

func (s *service) RemoveGrant(ctx context.Context, actor identity.Actor, organizationID, grantID string) error {
	return s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.grants.GetForUpdate(ctx, organizationID, grantID)
		if err != nil {
			return err
		}
		if g.IsOwnerGrant() {
			if err := s.checkNotLastOwner(ctx, g); err != nil {
				return err
			}
		}
		before := g.Snapshot()
		g.Remove()
		if err := s.grants.Delete(ctx, g); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateGrant, g.ID(), g); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: iam.EventGrantRemoved, OrganizationID: organizationID, ResourceID: g.ID(),
			Actor: actor, Before: before,
		})
	})
}

// checkNotLastOwner refuses to remove the owner grant of the Organization's
// only active owner. It locks the Organization first, so owners removed at
// the same time are counted one after the other.
func (s *service) checkNotLastOwner(ctx context.Context, g *iam.Grant) error {
	if _, err := s.orgs.GetForUpdate(ctx, g.OrganizationID()); err != nil {
		return err
	}
	if g.Expired() {
		return nil
	}
	p, err := s.principals.Get(ctx, g.OrganizationID(), *g.PrincipalID())
	if err != nil {
		return err
	}
	if p.Kind != iam.PrincipalUser || p.Status != iam.PrincipalActive {
		return nil
	}
	n, err := s.grants.CountOwners(ctx, g.OrganizationID(), clock.Now())
	if err != nil {
		return err
	}
	if n <= 1 {
		return fmt.Errorf("%w: %s", iam.ErrLastOwner, *g.PrincipalID())
	}
	return nil
}

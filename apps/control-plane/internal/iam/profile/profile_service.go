// Package profile tells a signed-in caller who they are: GET /v1/me.
package profile

import (
	"context"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type ProfileServiceOption func(*options)

type options struct {
	principals iam.PrincipalRepository
	users      iam.UserRepository
	accounts   iam.AccountRepository
	orgs       iam.OrganizationRepository
	access     iam.AccessRepository
	authz      authorization.Authorizer
}

func WithPrincipalRepo(r iam.PrincipalRepository) ProfileServiceOption {
	return func(o *options) { o.principals = r }
}

// WithUserRepo finds a user's Account, for its email.
func WithUserRepo(r iam.UserRepository) ProfileServiceOption {
	return func(o *options) { o.users = r }
}

func WithAccountRepo(r iam.AccountRepository) ProfileServiceOption {
	return func(o *options) { o.accounts = r }
}

func WithOrganizationRepo(r iam.OrganizationRepository) ProfileServiceOption {
	return func(o *options) { o.orgs = r }
}

// WithAccessRepo reads the caller's status and roles, as the authorizer does.
func WithAccessRepo(r iam.AccessRepository) ProfileServiceOption {
	return func(o *options) { o.access = r }
}

// WithAuthorizer answers which permissions the caller holds, through the
// same authorizer every operation asks.
func WithAuthorizer(a authorization.Authorizer) ProfileServiceOption {
	return func(o *options) { o.authz = a }
}

// NewProfileService returns the ProfileService. Every option is required.
func NewProfileService(opts ...ProfileServiceOption) (iam.ProfileService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"PrincipalRepo", o.principals != nil},
		{"UserRepo", o.users != nil},
		{"AccountRepo", o.accounts != nil},
		{"OrganizationRepo", o.orgs != nil},
		{"AccessRepo", o.access != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("profile service: %s is required", r.name)
		}
	}
	return &service{options: o}, nil
}

type service struct {
	options
}

func (s *service) Profile(ctx context.Context, actor identity.Actor, organizationID string) (*iam.Profile, error) {
	if organizationID == "" || actor.PrincipalID == "" {
		return nil, iam.NewErrInvalidInput("the caller has no principal or Organization")
	}

	// The Organization is its own resource, so the roles granted on it are
	// the caller's roles at Organization scope.
	access, err := s.access.Access(ctx, iam.AccessQuery{
		OrganizationID: organizationID,
		PrincipalID:    actor.PrincipalID,
		ResourceID:     organizationID,
		At:             clock.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("read access: %w", err)
	}
	switch {
	case !access.OrganizationActive:
		return nil, fmt.Errorf("organization is not active: %w", iam.ErrStatus)
	case access.PrincipalStatus == "":
		return nil, fmt.Errorf("principal: %w", iam.ErrNotFound)
	case access.PrincipalStatus != iam.PrincipalActive:
		return nil, fmt.Errorf("principal is %s: %w", access.PrincipalStatus, iam.ErrStatus)
	}

	principal, err := s.principals.Get(ctx, organizationID, actor.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("get principal: %w", err)
	}
	org, err := s.orgs.GetByIdentifier(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	p := &iam.Profile{
		Principal:    principal,
		Organization: org.Snapshot(),
		Roles:        roles(organizationID, access.RoleIDs),
	}

	if principal.Kind == iam.PrincipalUser {
		email, err := s.email(ctx, organizationID, actor.PrincipalID)
		if err != nil {
			return nil, err
		}
		p.Email = &email
	}
	return p, nil
}

// email returns the user's Account email, refusing an Account that is not
// active, as signing in does.
func (s *service) email(ctx context.Context, organizationID, principalID string) (string, error) {
	u, err := s.users.Get(ctx, organizationID, principalID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	a, err := s.accounts.Get(ctx, u.AccountID())
	if err != nil {
		return "", fmt.Errorf("get account: %w", err)
	}
	if a.Status() != iam.AccountActive {
		return "", fmt.Errorf("account is %s: %w", a.Status(), iam.ErrStatus)
	}
	return a.Email(), nil
}

func roles(resourceID string, roleIDs []string) []iam.HeldRole {
	out := make([]iam.HeldRole, 0, len(roleIDs))
	for _, id := range slices.Compact(slices.Sorted(slices.Values(roleIDs))) {
		key, _ := iam.BuiltInRoleKey(id)
		out = append(out, iam.HeldRole{Key: key, RoleID: id, ResourceID: resourceID})
	}
	return out
}

// MaxPermissionChecks bounds one Permissions call.
const MaxPermissionChecks = 20

// Permissions asks the authorizer about each permission in turn. A
// resource outside the Organization, or one that does not exist, holds
// none.
func (s *service) Permissions(ctx context.Context, actor identity.Actor, organizationID, resourceID string, permissions []string) ([]string, error) {
	if organizationID == "" || actor.PrincipalID == "" {
		return nil, iam.NewErrInvalidInput("the caller has no principal or Organization")
	}
	if resourceID == "" {
		resourceID = organizationID
	}
	asked := slices.Compact(slices.Sorted(slices.Values(permissions)))
	if len(asked) > MaxPermissionChecks {
		return nil, iam.NewErrInvalidInput(fmt.Sprintf("ask about at most %d permissions at once", MaxPermissionChecks))
	}
	granted := []string{}
	for _, permission := range asked {
		if permission == "" {
			continue
		}
		d, err := s.authz.Authorize(ctx, authorization.Request{
			Actor: actor, Permission: permission, OrganizationID: organizationID,
			ResourceID: resourceID, Action: iam.ActionPermissionCheck,
		})
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", permission, err)
		}
		if d.Outcome == authorization.OutcomeAllow {
			granted = append(granted, permission)
		}
	}
	return granted, nil
}

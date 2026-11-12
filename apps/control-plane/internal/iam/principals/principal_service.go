// Package principals names an Organization's principals by ID: GET
// /v1/principals. It resolves users, service accounts, Agents, agent
// Sessions, and workloads in one call, so a page can show who created or
// acted on something without knowing the kind first.
package principals

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// maxPrincipalIDs caps the distinct IDs one call names, so a page cannot
// ask for the whole Organization by ID. It is the only limit: repeated IDs
// do not count against it.
const maxPrincipalIDs = 100

type PrincipalServiceOption func(*options)

type options struct {
	principals iam.PrincipalReader
	teams      iam.PrincipalTeamSource
	authz      authorization.Authorizer
}

// WithPrincipalReader reads the principals the service names.
func WithPrincipalReader(r iam.PrincipalReader) PrincipalServiceOption {
	return func(o *options) { o.principals = r }
}

// WithPrincipalTeamSource names an Agent's Teams.
func WithPrincipalTeamSource(s iam.PrincipalTeamSource) PrincipalServiceOption {
	return func(o *options) { o.teams = s }
}

func WithAuthorizer(a authorization.Authorizer) PrincipalServiceOption {
	return func(o *options) { o.authz = a }
}

// NewPrincipalService returns the PrincipalService, which asks the
// authorizer before reading. Every option is required.
func NewPrincipalService(opts ...PrincipalServiceOption) (iam.PrincipalService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.principals == nil:
		return nil, errors.New("principal service: PrincipalReader is required")
	case o.teams == nil:
		return nil, errors.New("principal service: PrincipalTeamSource is required")
	case o.authz == nil:
		return nil, errors.New("principal service: Authorizer is required")
	}
	return &authorized{next: &service{principals: o.principals, teams: o.teams}, authz: o.authz}, nil
}

// authorized checks principal.read on the Organization before listing.
type authorized struct {
	next  iam.PrincipalService
	authz authorization.Authorizer
}

var _ iam.PrincipalService = (*authorized)(nil)

func (a *authorized) ListPrincipals(ctx context.Context, actor identity.Actor, q iam.ListPrincipalsQuery) ([]iam.PrincipalSummary, error) {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionPrincipalRead, OrganizationID: q.OrganizationID,
		ResourceID: q.OrganizationID, Action: iam.ActionPrincipalList,
	})
	if err != nil {
		return nil, err
	}
	return a.next.ListPrincipals(ctx, actor, q)
}

type service struct {
	principals iam.PrincipalReader
	teams      iam.PrincipalTeamSource
}

func (s *service) ListPrincipals(ctx context.Context, _ identity.Actor, q iam.ListPrincipalsQuery) ([]iam.PrincipalSummary, error) {
	ids := distinctIDs(q.IDs)
	if len(ids) > maxPrincipalIDs {
		return nil, iam.NewErrInvalidInput(fmt.Sprintf("at most %d principals may be named at once", maxPrincipalIDs))
	}
	if len(ids) == 0 {
		return []iam.PrincipalSummary{}, nil
	}
	found, err := s.principals.ListByIDs(ctx, q.OrganizationID, ids)
	if err != nil {
		return nil, err
	}

	// Keep the order the caller asked for, and leave out the IDs that name
	// no principal of the Organization.
	byID := make(map[string]iam.PrincipalSnapshot, len(found))
	for _, p := range found {
		byID[p.ID] = p
	}
	out := make([]iam.PrincipalSummary, 0, len(found))
	agentIDs := make([]string, 0, len(found))
	for _, id := range ids {
		p, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, iam.PrincipalSummary{Principal: p})
		if p.Kind == iam.PrincipalAgent {
			agentIDs = append(agentIDs, p.ID)
		}
	}
	if len(agentIDs) == 0 {
		return out, nil
	}

	teams, err := s.teams.ListTeams(ctx, q.OrganizationID, agentIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Principal.Kind == iam.PrincipalAgent {
			out[i].Teams = teams[out[i].Principal.ID]
		}
	}
	return out, nil
}

// distinctIDs drops empty and repeated IDs, keeping the order they were
// asked for.
func distinctIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

package grants

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org       = "org_01JZ0000000000000000000001"
	workspace = "wsp_01JZ0000000000000000000001"
	project   = "prj_01JZ0000000000000000000001"
	archived  = "wsp_01JZ0000000000000000000002"
	ada       = "prn_01JZ00000000000000000000AD"
	grace     = "prn_01JZ00000000000000000000GR"
	linus     = "prn_01JZ00000000000000000000LI"
	bot       = "prn_01JZ00000000000000000000B0"
	session   = "prn_01JZ00000000000000000000SE"
	deployers = "grp_01JZ0000000000000000000001"
	platform  = "tem_01JZ0000000000000000000001"
	teamGroup = "grp_01JZ0000000000000000000002"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// store fakes the repositories, the Team group source, and the
// transactor, which puts the grants back when a transaction fails. Its
// auditLog and outboxLog fake the recorders. It embeds the interfaces it
// fakes only in part, so a call to anything else panics.
type store struct {
	iam.GrantRepository

	grants     map[string]iam.GrantSnapshot
	resources  map[string]iam.Resource
	principals map[string]iam.PrincipalSnapshot
	groups     map[string]string
	teams      map[string]string
	audit      []audit.Event
	events     []events.Envelope
	// orgLocks counts the times the Organization was locked.
	orgLocks int
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	grants, auditLen, eventsLen := maps.Clone(s.grants), len(s.audit), len(s.events)
	if err := fn(ctx); err != nil {
		s.grants, s.audit, s.events = grants, s.audit[:auditLen], s.events[:eventsLen]
		return err
	}
	return nil
}

func load(snap iam.GrantSnapshot) *iam.Grant {
	return must(iam.NewGrant(iam.NewGrantProps{
		OrganizationID: snap.OrganizationID, PrincipalID: snap.PrincipalID, GroupID: snap.GroupID,
		RoleID: snap.RoleID, ResourceID: snap.ResourceID, GrantedByPrincipalID: snap.GrantedByPrincipalID,
		ExpiresAt: snap.ExpiresAt, CreatedAt: &snap.CreatedAt,
	}, &snap.ID))
}

// Grants.

func (s *store) Write(_ context.Context, g *iam.Grant) error {
	n := g.Snapshot()
	if n.GroupID != nil && s.groups[*n.GroupID] == "" {
		return iam.ErrNotFound
	}
	for _, e := range s.grants {
		if e.RoleID == n.RoleID && e.ResourceID == n.ResourceID &&
			ptrEq(e.PrincipalID, n.PrincipalID) && ptrEq(e.GroupID, n.GroupID) {
			return iam.ErrAlreadyExists
		}
	}
	s.grants[n.ID] = n
	return nil
}

func ptrEq(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func (s *store) Get(_ context.Context, organizationID, grantID string) (*iam.Grant, error) {
	snap, ok := s.grants[grantID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, iam.ErrNotFound
	}
	return load(snap), nil
}

func (s *store) GetForUpdate(ctx context.Context, organizationID, grantID string) (*iam.Grant, error) {
	return s.Get(ctx, organizationID, grantID)
}

func (s *store) Delete(_ context.Context, g *iam.Grant) error {
	delete(s.grants, g.ID())
	return nil
}

func (s *store) CountOwners(_ context.Context, organizationID string, at time.Time) (int, error) {
	owner, _ := iam.BuiltInRoleID(iam.RoleOwner)
	n := 0
	for _, g := range s.grants {
		if g.OrganizationID != organizationID || g.RoleID != owner || g.ResourceID != organizationID || g.PrincipalID == nil {
			continue
		}
		if g.ExpiresAt != nil && !g.ExpiresAt.After(at) {
			continue
		}
		p := s.principals[*g.PrincipalID]
		if p.Kind == iam.PrincipalUser && p.Status == iam.PrincipalActive {
			n++
		}
	}
	return n, nil
}

func (s *store) ListGrants(_ context.Context, q iam.ListGrantsQuery) (*pagination.Result[iam.GrantEntry], error) {
	var items []iam.GrantEntry
	for _, g := range s.grants {
		if g.OrganizationID != q.OrganizationID || !slices.Contains(q.ResourceIDs, g.ResourceID) {
			continue
		}
		if q.GrantID != "" && g.ID != q.GrantID {
			continue
		}
		if !q.IncludeExpired && g.ExpiresAt != nil && !g.ExpiresAt.After(q.At) {
			continue
		}
		key, _ := iam.BuiltInRoleKey(g.RoleID)
		e := iam.GrantEntry{Grant: g, RoleKey: key}
		if g.PrincipalID != nil {
			p := s.principals[*g.PrincipalID]
			e.Principal = &iam.GranteePrincipal{ID: p.ID, Kind: p.Kind, DisplayName: p.DisplayName, Status: p.Status}
		}
		if g.GroupID != nil {
			e.Group = &iam.GranteeGroup{ID: *g.GroupID, Name: s.groups[*g.GroupID], Source: "manual"}
		}
		items = append(items, e)
	}
	slices.SortFunc(items, func(a, b iam.GrantEntry) int { return cmp.Compare(a.Grant.ID, b.Grant.ID) })
	return &pagination.Result[iam.GrantEntry]{Items: items, TotalCount: len(items)}, nil
}

// Resources.

type resources struct{ *store }

func (r resources) GetResource(_ context.Context, organizationID, resourceID string) (iam.Resource, error) {
	res, ok := r.resources[resourceID]
	if !ok || res.OrganizationID != organizationID {
		return iam.Resource{}, iam.ErrNotFound
	}
	return res, nil
}

func (r resources) ResourceAncestors(ctx context.Context, organizationID, resourceID string) ([]string, error) {
	var ids []string
	for id := resourceID; id != ""; {
		res, err := r.GetResource(ctx, organizationID, id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		id = res.ParentID
	}
	return ids, nil
}

// Principals.

type principals struct{ *store }

func (p principals) Get(_ context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error) {
	snap, ok := p.principals[principalID]
	if !ok || snap.OrganizationID != organizationID {
		return iam.PrincipalSnapshot{}, iam.ErrNotFound
	}
	return snap, nil
}

// Organizations.

type orgs struct {
	iam.OrganizationRepository
	s *store
}

func (o orgs) GetForUpdate(_ context.Context, organizationID string) (*iam.Organization, error) {
	o.s.orgLocks++
	return must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, new(string(organizationID)))), nil
}

// Teams.

type teams struct{ *store }

func (t teams) TeamGroupID(_ context.Context, _, teamID string) (string, error) {
	if teamID == "tem_archived" {
		return "", iam.ErrStatus
	}
	groupID, ok := t.teams[teamID]
	if !ok {
		return "", iam.ErrNotFound
	}
	return groupID, nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.events = append(o.s.events, envs...)
	return nil
}

func (s *store) eventTypes() []string {
	var out []string
	for _, e := range s.events {
		out = append(out, e.Type)
	}
	return out
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's action.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
		d.Reason = "denied by the test"
	}
	return d, nil
}

// world is Acme with a Workspace, a Project under it, and an archived
// Workspace; Ada (an owner), Grace, suspended Linus, an Agent, and an
// Agent Session; the deployers group; and the Platform Team.
type world struct {
	*store
	svc   iam.GrantService
	authz *authorizer
	owner *iam.Grant
}

func person(id string, kind iam.PrincipalKind, name string, status iam.PrincipalStatus) iam.PrincipalSnapshot {
	return iam.PrincipalSnapshot{ID: id, OrganizationID: org, Kind: kind, DisplayName: name, Status: status}
}

func role(key string) string {
	id, _ := iam.BuiltInRoleID(key)
	return id
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := &store{
		grants: map[string]iam.GrantSnapshot{},
		resources: map[string]iam.Resource{
			org:       {ID: org, OrganizationID: org, Type: iam.ResourceOrganization},
			workspace: {ID: workspace, OrganizationID: org, Type: iam.ResourceWorkspace, ParentID: org},
			project:   {ID: project, OrganizationID: org, Type: iam.ResourceProject, ParentID: workspace},
			archived:  {ID: archived, OrganizationID: org, Type: iam.ResourceWorkspace, ParentID: org, Archived: true},
		},
		principals: map[string]iam.PrincipalSnapshot{
			ada:     person(ada, iam.PrincipalUser, "Ada", iam.PrincipalActive),
			grace:   person(grace, iam.PrincipalUser, "Grace", iam.PrincipalActive),
			linus:   person(linus, iam.PrincipalUser, "Linus", iam.PrincipalSuspended),
			bot:     person(bot, iam.PrincipalAgent, "Release Bot", iam.PrincipalActive),
			session: person(session, iam.PrincipalAgentSession, "Session", iam.PrincipalActive),
		},
		groups: map[string]string{deployers: "deployers", teamGroup: "team:platform"},
		teams:  map[string]string{platform: teamGroup},
	}
	owner := must(iam.NewOwnerGrant(org, ada))
	s.grants[owner.ID()] = owner.Snapshot()

	authz := &authorizer{}
	svc, err := NewGrantService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz, owner: owner}
}

func (s *store) options(authz authorization.Authorizer) []GrantServiceOption {
	return []GrantServiceOption{
		WithGrantRepo(s), WithResourceRepo(resources{s}), WithPrincipalRepo(principals{s}),
		WithOrganizationRepo(orgs{s: s}), WithTeamGroupSource(teams{s}), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

var adaActor = identity.Actor{PrincipalID: ada, Kind: identity.ActorUser}

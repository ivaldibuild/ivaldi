package registries

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const org = "org_acme"

// state is everything the fakes store. A transaction restores it when its
// function fails, as a rolled-back database transaction would.
type state struct {
	registries map[string]delivery.RegistrySnapshot
	audit      []audit.Event
	events     []events.Envelope
}

func (s state) clone() state {
	return state{
		registries: maps.Clone(s.registries),
		audit:      append([]audit.Event(nil), s.audit...),
		events:     append([]events.Envelope(nil), s.events...),
	}
}

// store fakes the repository, the transactor, and the Workspaces lookup. Its
// auditLog and outboxLog fake the recorders.
type store struct {
	state
	workspaces map[string]*workspace.Workspace
	// auditErr makes every audit record fail.
	auditErr error
}

func newStore() *store {
	return &store{
		state:      state{registries: map[string]delivery.RegistrySnapshot{}},
		workspaces: map[string]*workspace.Workspace{},
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

func rebuildRegistry(s delivery.RegistrySnapshot) *delivery.Registry {
	return must(delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID, Slug: s.Slug, DisplayName: s.DisplayName,
		Kind: s.Kind, URL: s.URL, CredentialRef: s.CredentialRef, LookupFrom: s.LookupFrom, LookupPool: s.LookupPool,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

// slugTaken is the unique index on active Registries' slugs in a Workspace.
func (s *store) slugTaken(snap delivery.RegistrySnapshot) bool {
	for _, o := range s.registries {
		if o.ID != snap.ID && o.WorkspaceID == snap.WorkspaceID && o.Slug == snap.Slug && o.DeletedAt == nil && snap.DeletedAt == nil {
			return true
		}
	}
	return false
}

func (s *store) CreateRegistry(_ context.Context, r *delivery.Registry) error {
	snap := r.Snapshot()
	if s.slugTaken(snap) {
		return delivery.ErrSlugTaken
	}
	s.registries[snap.ID] = snap
	return nil
}

func (s *store) GetRegistry(_ context.Context, organizationID, id string) (*delivery.Registry, error) {
	snap, ok := s.registries[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildRegistry(snap), nil
}

func (s *store) GetRegistryBySlug(_ context.Context, organizationID, workspaceID, slug string) (*delivery.Registry, error) {
	for _, snap := range s.registries {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.Slug == slug && snap.DeletedAt == nil {
			return rebuildRegistry(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListRegistries(_ context.Context, q delivery.ListRegistriesQuery) (*pagination.Result[*delivery.Registry], error) {
	out := &pagination.Result[*delivery.Registry]{}
	for _, snap := range s.registries {
		if snap.OrganizationID == q.OrganizationID && snap.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || snap.DeletedAt == nil) {
			out.Items = append(out.Items, rebuildRegistry(snap))
		}
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) UpdateRegistry(_ context.Context, r *delivery.Registry) error {
	if _, ok := s.registries[r.ID()]; !ok {
		return delivery.ErrNotFound
	}
	snap := r.Snapshot()
	if s.slugTaken(snap) {
		return delivery.ErrSlugTaken
	}
	s.registries[r.ID()] = snap
	return nil
}

func (s *store) CountActiveRegistries(_ context.Context, organizationID, workspaceID string) (int, error) {
	n := 0
	for _, snap := range s.registries {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *store) LookupWorkspace(_ context.Context, _, workspaceID string) (*workspace.Workspace, error) {
	w, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return w, nil
}

func (s *store) LookupProject(context.Context, string, string) (*workspace.Project, error) {
	return nil, errors.New("not used by registries")
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	if a.s.auditErr != nil {
		return a.s.auditErr
	}
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

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

func (s *store) lastAudit() audit.Event {
	return s.audit[len(s.audit)-1]
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's
// resource ID. outcome, when set, replaces the allow.
type authorizer struct {
	asked   []authorization.Request
	deny    func(authorization.Request) bool
	outcome authorization.Outcome
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
	if a.outcome != "" {
		d.Outcome = a.outcome
	}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

// world is a store with an active and an archived Workspace, and the
// RegistryService over it.
type world struct {
	*store
	authz *authorizer
	svc   delivery.RegistryService
}

const (
	wsActive   = "wsp_01JZ0000000000000000000001"
	wsArchived = "wsp_01JZ0000000000000000000002"
	wsOther    = "wsp_01JZ0000000000000000000003"
)

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	for _, ws := range []struct {
		id, slug string
		archived bool
	}{{wsActive, "platform", false}, {wsArchived, "legacy", true}, {wsOther, "data", false}} {
		id := ws.id
		w := must(workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: ws.slug, DisplayName: ws.slug}, &id))
		if ws.archived {
			require.NoError(t, w.Archive())
		}
		s.workspaces[id] = w
	}
	authz := &authorizer{}
	svc, err := NewRegistryService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []RegistryServiceOption {
	return []RegistryServiceOption{
		WithRegistryRepo(s), WithWorkspaces(s), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// registry stores a Registry directly, bypassing the service.
func (w *world) registry(t *testing.T, workspaceID, slug string, archived bool) *delivery.Registry {
	t.Helper()
	r := must(delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Kind: delivery.RegistryOCI, URL: "https://ghcr.io",
	}, nil))
	if archived {
		r.Archive()
	}
	r.PullEvents()
	require.NoError(t, w.CreateRegistry(context.Background(), r))
	return r
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.authz.asked = nil, nil, nil
}

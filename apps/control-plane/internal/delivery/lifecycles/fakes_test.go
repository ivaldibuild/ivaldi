package lifecycles

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	lifecycles   map[string]delivery.LifecycleSnapshot
	environments map[string]delivery.EnvironmentSnapshot
	audit        []audit.Event
	events       []events.Envelope
}

func (s state) clone() state {
	return state{
		lifecycles:   maps.Clone(s.lifecycles),
		environments: maps.Clone(s.environments),
		audit:        append([]audit.Event(nil), s.audit...),
		events:       append([]events.Envelope(nil), s.events...),
	}
}

// store fakes the repository, the Environment and Channel lookups, the
// transactor, and the Workspaces lookup. Its auditLog and outboxLog fake the
// recorders.
type store struct {
	state
	workspaces map[string]*workspace.Workspace
	// channelUse counts the active Channels that follow each Lifecycle.
	channelUse map[string]int
}

func newStore() *store {
	return &store{
		state: state{
			lifecycles:   map[string]delivery.LifecycleSnapshot{},
			environments: map[string]delivery.EnvironmentSnapshot{},
		},
		workspaces: map[string]*workspace.Workspace{},
		channelUse: map[string]int{},
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type txKey struct{}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(context.WithValue(ctx, txKey{}, true)); err != nil {
		s.state = saved
		return err
	}
	return nil
}

// Lifecycles.

func rebuildLifecycle(s delivery.LifecycleSnapshot) *delivery.Lifecycle {
	phases := make([]delivery.NewLifecyclePhaseProps, len(s.Phases))
	for i, p := range s.Phases {
		phases[i] = delivery.NewLifecyclePhaseProps{
			ID: &p.ID, Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments,
			EnvironmentIDs: p.EnvironmentIDs, CreatedAt: &p.CreatedAt, UpdatedAt: &p.UpdatedAt,
		}
	}
	return must(delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID, Slug: s.Slug, DisplayName: s.DisplayName,
		Description: s.Description, Phases: phases,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

// slugTaken is the unique index on active Lifecycles' slugs in a Workspace.
func (s *store) slugTaken(snap delivery.LifecycleSnapshot) bool {
	for _, o := range s.lifecycles {
		if o.ID != snap.ID && o.WorkspaceID == snap.WorkspaceID && o.Slug == snap.Slug && o.DeletedAt == nil && snap.DeletedAt == nil {
			return true
		}
	}
	return false
}

func (s *store) CreateLifecycle(_ context.Context, l *delivery.Lifecycle) error {
	snap := l.Snapshot()
	if s.slugTaken(snap) {
		return delivery.ErrSlugTaken
	}
	s.lifecycles[snap.ID] = snap
	return nil
}

func (s *store) GetLifecycle(_ context.Context, organizationID, id string) (*delivery.Lifecycle, error) {
	snap, ok := s.lifecycles[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildLifecycle(snap), nil
}

// GetLifecycleForUpdate fails outside a transaction, as FOR UPDATE would be
// pointless there.
func (s *store) GetLifecycleForUpdate(ctx context.Context, organizationID, id string) (*delivery.Lifecycle, error) {
	if ctx.Value(txKey{}) == nil {
		return nil, fmt.Errorf("GetLifecycleForUpdate called outside a transaction")
	}
	return s.GetLifecycle(ctx, organizationID, id)
}

func (s *store) GetLifecycleBySlug(_ context.Context, organizationID, workspaceID, slug string) (*delivery.Lifecycle, error) {
	for _, snap := range s.lifecycles {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.Slug == slug && snap.DeletedAt == nil {
			return rebuildLifecycle(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListLifecycles(_ context.Context, q delivery.ListLifecyclesQuery) (*pagination.Result[*delivery.Lifecycle], error) {
	out := &pagination.Result[*delivery.Lifecycle]{}
	for _, snap := range s.lifecycles {
		if snap.OrganizationID == q.OrganizationID && snap.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || snap.DeletedAt == nil) {
			out.Items = append(out.Items, rebuildLifecycle(snap))
		}
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) UpdateLifecycle(_ context.Context, l *delivery.Lifecycle) error {
	if _, ok := s.lifecycles[l.ID()]; !ok {
		return delivery.ErrNotFound
	}
	snap := l.Snapshot()
	if s.slugTaken(snap) {
		return delivery.ErrSlugTaken
	}
	s.lifecycles[l.ID()] = snap
	return nil
}

func (s *store) CountActiveLifecycles(_ context.Context, organizationID, workspaceID string) (int, error) {
	n := 0
	for _, snap := range s.lifecycles {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *store) CountActiveLifecyclesWithEnvironment(_ context.Context, organizationID, environmentID string) (int, error) {
	n := 0
	for _, snap := range s.lifecycles {
		if snap.OrganizationID != organizationID || snap.DeletedAt != nil {
			continue
		}
		for _, p := range snap.Phases {
			if slices.Contains(p.EnvironmentIDs, environmentID) {
				n++
				break
			}
		}
	}
	return n, nil
}

// Environments and Channels.

func (s *store) GetEnvironments(_ context.Context, organizationID string, ids []string) ([]*delivery.Environment, error) {
	var out []*delivery.Environment
	for _, id := range ids {
		snap, ok := s.environments[id]
		if !ok || snap.OrganizationID != organizationID {
			continue
		}
		out = append(out, must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: snap.OrganizationID, WorkspaceID: snap.WorkspaceID, Slug: snap.Slug, DisplayName: snap.DisplayName,
			Tier: snap.Tier, ExecutionEnabled: snap.ExecutionEnabled,
			CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt, DeletedAt: snap.DeletedAt,
		}, &snap.ID)))
	}
	return out, nil
}

func (s *store) CountActiveChannelsWithLifecycle(_ context.Context, _, lifecycleID string) (int, error) {
	return s.channelUse[lifecycleID], nil
}

// Workspaces.

func (s *store) LookupWorkspace(_ context.Context, _, workspaceID string) (*workspace.Workspace, error) {
	w, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return w, nil
}

func (s *store) LookupProject(context.Context, string, string) (*workspace.Project, error) {
	return nil, errors.New("not used by lifecycles")
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

// world is a store with two active Workspaces and an archived one, and the
// LifecycleService over it.
type world struct {
	*store
	authz *authorizer
	svc   delivery.LifecycleService
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
	svc, err := NewLifecycleService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []LifecycleServiceOption {
	return []LifecycleServiceOption{
		WithLifecycleRepo(s), WithEnvironmentLookup(s), WithChannelUsage(s), WithWorkspaces(s),
		WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// env stores an Environment directly: the environments package owns them.
func (w *world) env(t *testing.T, workspaceID, slug string, archived bool) *delivery.Environment {
	t.Helper()
	e := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Tier: delivery.TierStaging,
	}, nil))
	if archived {
		e.Archive()
	}
	w.setEnv(e)
	return e
}

func (w *world) setEnv(e *delivery.Environment) {
	e.PullEvents()
	w.environments[e.ID()] = e.Snapshot()
}

// lifecycle stores a Lifecycle directly, with one phase per Environment.
func (w *world) lifecycle(t *testing.T, workspaceID, slug string, archived bool, envs ...*delivery.Environment) *delivery.Lifecycle {
	t.Helper()
	phases := make([]delivery.NewLifecyclePhaseProps, len(envs))
	for i, e := range envs {
		phases[i] = delivery.NewLifecyclePhaseProps{Name: e.Slug(), EnvironmentIDs: []string{e.ID()}}
	}
	l := must(delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Phases: phases,
	}, nil))
	if archived {
		l.Archive()
	}
	l.PullEvents()
	require.NoError(t, w.CreateLifecycle(context.Background(), l))
	return l
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.authz.asked = nil, nil, nil
}

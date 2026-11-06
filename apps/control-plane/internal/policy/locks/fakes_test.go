package locks

import (
	"context"
	"maps"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	org          = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	lifecycleKey = "delivery.default_lifecycle"
	// workspaceOnlyKey may be set only at Workspace level, so only an
	// Organization can lock it.
	workspaceOnlyKey = "delivery.workspace_only"
)

var (
	ada      = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}
	agent    = identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorAgentSession, SessionID: "ses_1"}
	fixedNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
)

// state is what a transaction restores when it fails.
type state struct {
	Locks    map[string]policy.SettingLockSnapshot
	AuditLog []audit.Event
	Events   []events.Envelope
}

func (s state) clone() state {
	return state{Locks: maps.Clone(s.Locks), AuditLog: slices.Clone(s.AuditLog), Events: slices.Clone(s.Events)}
}

// store is an in-memory policy.SettingLockRepository, with the uniqueness
// rule the migration enforces, and also the Transactor, the Workspace
// lookups, and the recorders. It stores snapshots and returns rebuilt locks.
type store struct {
	state
	// Workspaces holds the Workspaces in the Organization.
	Workspaces map[string]*workspace.Workspace
}

var (
	_ policy.SettingLockRepository = (*store)(nil)
	_ policy.Workspaces            = (*store)(nil)
)

func newStore() *store {
	return &store{state: state{Locks: map[string]policy.SettingLockSnapshot{}}, Workspaces: map[string]*workspace.Workspace{}}
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

func rebuild(s policy.SettingLockSnapshot) *policy.SettingLock {
	l, err := policy.NewSettingLock(policy.NewSettingLockProps{
		Scope: policy.LockScope{OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID}, Key: s.Key,
		LockedByPrincipalID: s.LockedByPrincipalID, CreatedAt: &s.CreatedAt,
	}, &s.ID)
	if err != nil {
		panic(err)
	}
	return l
}

func (s *store) CreateSettingLock(_ context.Context, l *policy.SettingLock) error {
	snap := l.Snapshot()
	for _, o := range s.Locks {
		if o.OrganizationID == snap.OrganizationID && o.WorkspaceID == snap.WorkspaceID && o.Key == snap.Key {
			return policy.ErrConflict
		}
	}
	s.Locks[snap.ID] = snap
	return nil
}

func (s *store) GetSettingLock(_ context.Context, scope policy.LockScope, key string) (*policy.SettingLock, error) {
	for _, o := range s.Locks {
		if o.OrganizationID == scope.OrganizationID && o.WorkspaceID == scope.WorkspaceID && o.Key == key {
			return rebuild(o), nil
		}
	}
	return nil, policy.ErrNotFound
}

func (s *store) list(match func(policy.SettingLockSnapshot) bool) []*policy.SettingLock {
	var found []policy.SettingLockSnapshot
	for _, o := range s.Locks {
		if match(o) {
			found = append(found, o)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Key != found[j].Key {
			return found[i].Key < found[j].Key
		}
		return found[i].WorkspaceID < found[j].WorkspaceID
	})
	out := make([]*policy.SettingLock, 0, len(found))
	for _, o := range found {
		out = append(out, rebuild(o))
	}
	return out
}

func (s *store) ListSettingLocks(_ context.Context, scope policy.LockScope) ([]*policy.SettingLock, error) {
	return s.list(func(o policy.SettingLockSnapshot) bool {
		return o.OrganizationID == scope.OrganizationID && o.WorkspaceID == scope.WorkspaceID
	}), nil
}

func (s *store) ListLocksInEffect(_ context.Context, scope policy.LockScope) ([]*policy.SettingLock, error) {
	return s.list(func(o policy.SettingLockSnapshot) bool {
		return o.OrganizationID == scope.OrganizationID && (o.WorkspaceID == "" || o.WorkspaceID == scope.WorkspaceID)
	}), nil
}

func (s *store) DeleteSettingLock(_ context.Context, organizationID, lockID string) error {
	o, ok := s.Locks[lockID]
	if !ok || o.OrganizationID != organizationID {
		return policy.ErrNotFound
	}
	delete(s.Locks, lockID)
	return nil
}

func (s *store) LookupWorkspace(_ context.Context, organizationID, workspaceID string) (*workspace.Workspace, error) {
	w, ok := s.Workspaces[workspaceID]
	if !ok || w.OrganizationID() != organizationID {
		return nil, workspace.ErrNotFound
	}
	return w, nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.AuditLog = append(a.s.AuditLog, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.Events = append(o.s.Events, envs...)
	return nil
}

func (s *store) eventTypes() []string {
	out := make([]string, 0, len(s.Events))
	for _, e := range s.Events {
		out = append(out, e.Type)
	}
	return out
}

// authorizer allows everything unless decide is set, and records every
// request. Its default decision's ID is "dec-" and the request's resource ID.
type authorizer struct {
	asked  []authorization.Request
	decide func(authorization.Request) authorization.Decision
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	if a.decide != nil {
		return a.decide(req), nil
	}
	return authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}, nil
}

func deny(authorization.Request) authorization.Decision {
	return authorization.Decision{ID: "dec-deny", Outcome: authorization.OutcomeDeny}
}

// world is a store with an active and an archived Workspace, two setting
// definitions, and the service over them. Tests that use it fix the clock,
// so they must not run in parallel.
type world struct {
	*store
	authz       *authorizer
	definitions *workspace.Definitions
	svc         policy.SettingLockService
	ws          *workspace.Workspace
	archived    *workspace.Workspace
}

func (s *store) options(defs *workspace.Definitions, authz authorization.Authorizer) []SettingLockServiceOption {
	return []SettingLockServiceOption{
		WithSettingLockRepo(s), WithDefinitions(defs), WithWorkspaces(s), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, fixedNow)
	s := newStore()
	defs := workspace.NewDefinitions()
	require.NoError(t, defs.Register(workspace.Definition{Key: lifecycleKey, Levels: []workspace.Level{workspace.LevelWorkspace, workspace.LevelProject}, Permission: "lifecycle.manage"}))
	require.NoError(t, defs.Register(workspace.Definition{Key: workspaceOnlyKey, Levels: []workspace.Level{workspace.LevelWorkspace}, Permission: "p"}))
	authz := &authorizer{}
	svc, err := NewSettingLockService(s.options(defs, authz)...)
	require.NoError(t, err)
	w := &world{store: s, authz: authz, definitions: defs, svc: svc}
	w.ws = w.workspace(t, "platform", false)
	w.archived = w.workspace(t, "old", true)
	return w
}

func (w *world) workspace(t *testing.T, slug string, archived bool) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: slug, DisplayName: slug}, nil)
	require.NoError(t, err)
	if archived {
		require.NoError(t, ws.Archive())
	}
	w.Workspaces[ws.ID()] = ws
	return ws
}

func (w *world) orgScope() policy.LockScope { return policy.OrganizationLockScope(org) }

func (w *world) wsScope() policy.LockScope { return policy.WorkspaceLockScope(org, w.ws.ID()) }

// reset forgets what was recorded and asked so far.
func (w *world) reset() {
	w.AuditLog, w.Events, w.authz.asked = nil, nil, nil
}

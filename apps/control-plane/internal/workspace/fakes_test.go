package workspace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// fakeRepo is an in-memory Repository with the uniqueness rules the
// migrations enforce. Like the PostgreSQL repository, it stores state and
// returns rebuilt objects, so callers never share an object with it.
type fakeRepo struct {
	mu         sync.Mutex
	workspaces map[string]WorkspaceData
	projects   map[string]ProjectData
	settings   map[string]SettingData
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{workspaces: map[string]WorkspaceData{}, projects: map[string]ProjectData{}, settings: map[string]SettingData{}}
}

func settingKey(s Scope, key string) string {
	return s.OrganizationID + "/" + s.WorkspaceID + "/" + s.ProjectID + "/" + key
}

func (r *fakeRepo) checkWorkspace(w WorkspaceData) error {
	for _, o := range r.workspaces {
		if o.ID == w.ID || o.OrganizationID != w.OrganizationID {
			continue
		}
		if o.ArchivedAt == nil && w.ArchivedAt == nil && o.Slug == w.Slug {
			return ErrSlugTaken
		}
		if o.IsDefault && w.IsDefault {
			return ErrConflict
		}
	}
	if w.IsDefault && w.ArchivedAt != nil {
		return ErrDefaultWorkspace
	}
	return nil
}

func (r *fakeRepo) CreateWorkspace(_ context.Context, w *Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := w.Data()
	if err := r.checkWorkspace(d); err != nil {
		return err
	}
	r.workspaces[d.ID] = d
	return nil
}

func (r *fakeRepo) GetWorkspace(_ context.Context, org, wsID string) (*Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.workspaces[wsID]
	if !ok || d.OrganizationID != org {
		return nil, ErrNotFound
	}
	return ReconstituteWorkspace(d), nil
}

func (r *fakeRepo) GetWorkspaceForUpdate(ctx context.Context, org, wsID string) (*Workspace, error) {
	return r.GetWorkspace(ctx, org, wsID)
}

func (r *fakeRepo) GetDefaultWorkspace(_ context.Context, org string) (*Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.workspaces {
		if d.OrganizationID == org && d.IsDefault {
			return ReconstituteWorkspace(d), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeRepo) ListWorkspaces(_ context.Context, org string, includeArchived bool) ([]*Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Workspace
	for _, d := range r.workspaces {
		if d.OrganizationID == org && (includeArchived || d.ArchivedAt == nil) {
			out = append(out, ReconstituteWorkspace(d))
		}
	}
	return out, nil
}

func (r *fakeRepo) UpdateWorkspace(_ context.Context, w *Workspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := w.Data()
	if o, ok := r.workspaces[d.ID]; !ok || o.OrganizationID != d.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkWorkspace(d); err != nil {
		return err
	}
	r.workspaces[d.ID] = d
	return nil
}

func (r *fakeRepo) CountActiveProjects(_ context.Context, org, wsID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, d := range r.projects {
		if d.OrganizationID == org && d.WorkspaceID == wsID && d.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) checkProject(p ProjectData) error {
	for _, o := range r.projects {
		if o.ID != p.ID && o.OrganizationID == p.OrganizationID && o.WorkspaceID == p.WorkspaceID &&
			o.ArchivedAt == nil && p.ArchivedAt == nil && o.Slug == p.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeRepo) CreateProject(_ context.Context, p *Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := p.Data()
	if err := r.checkProject(d); err != nil {
		return err
	}
	r.projects[d.ID] = d
	return nil
}

func (r *fakeRepo) GetProject(_ context.Context, org, prjID string) (*Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.projects[prjID]
	if !ok || d.OrganizationID != org {
		return nil, ErrNotFound
	}
	return ReconstituteProject(d), nil
}

func (r *fakeRepo) ListProjects(_ context.Context, org, wsID string, includeArchived bool) ([]*Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Project
	for _, d := range r.projects {
		if d.OrganizationID == org && d.WorkspaceID == wsID && (includeArchived || d.ArchivedAt == nil) {
			out = append(out, ReconstituteProject(d))
		}
	}
	return out, nil
}

func (r *fakeRepo) UpdateProject(_ context.Context, p *Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := p.Data()
	if o, ok := r.projects[d.ID]; !ok || o.OrganizationID != d.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkProject(d); err != nil {
		return err
	}
	r.projects[d.ID] = d
	return nil
}

func (r *fakeRepo) GetSetting(_ context.Context, s Scope, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.settings[settingKey(s, key)]
	if !ok {
		return nil, ErrNotFound
	}
	return ReconstituteSetting(d), nil
}

func (r *fakeRepo) ListSettings(_ context.Context, s Scope) ([]*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Setting
	for _, d := range r.settings {
		if d.Scope == s {
			out = append(out, ReconstituteSetting(d))
		}
	}
	return out, nil
}

func (r *fakeRepo) SettingsForResolution(_ context.Context, s Scope, key string) ([]*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Setting
	for _, scope := range []Scope{
		OrganizationScope(s.OrganizationID),
		WorkspaceScope(s.OrganizationID, s.WorkspaceID),
		s,
	} {
		if scope.Level().rank() > s.Level().rank() {
			continue
		}
		if d, ok := r.settings[settingKey(scope, key)]; ok {
			out = append(out, ReconstituteSetting(d))
		}
	}
	return out, nil
}

func (r *fakeRepo) InsertSetting(_ context.Context, s *Setting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := s.Data()
	k := settingKey(d.Scope, d.Key)
	if _, ok := r.settings[k]; ok {
		return ErrConflict
	}
	r.settings[k] = d
	return nil
}

func (r *fakeRepo) UpdateSetting(_ context.Context, s *Setting, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := s.Data()
	k := settingKey(d.Scope, d.Key)
	if cur, ok := r.settings[k]; !ok || cur.Version != expectedVersion {
		return ErrConflict
	}
	r.settings[k] = d
	return nil
}

func (r *fakeRepo) DeleteSetting(_ context.Context, s Scope, key string, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := settingKey(s, key)
	if cur, ok := r.settings[k]; !ok || cur.Version != expectedVersion {
		return ErrConflict
	}
	delete(r.settings, k)
	return nil
}

// fakeTx runs fn directly. Atomicity is tested against PostgreSQL in the postgres package.
type fakeTx struct{}

func (fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// fakeAuthz allows everything unless decide is set, and records every request.
type fakeAuthz struct {
	mu       sync.Mutex
	decide   func(authorization.Request) authorization.Decision
	requests []authorization.Request
}

func (a *fakeAuthz) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	if a.decide != nil {
		return a.decide(req), nil
	}
	return authorization.Decision{ID: "dec_allow", Outcome: authorization.OutcomeAllow}, nil
}

func (a *fakeAuthz) last() authorization.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests[len(a.requests)-1]
}

type resourceCall struct {
	op string
	iam.Resource
}

type fakeResources struct {
	mu    sync.Mutex
	calls []resourceCall
}

func (f *fakeResources) RegisterResource(_ context.Context, r iam.Resource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, resourceCall{"register", r})
	return nil
}

func (f *fakeResources) ArchiveResource(_ context.Context, org, rid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, resourceCall{"archive", iam.Resource{ID: rid, OrganizationID: org}})
	return nil
}

func (f *fakeResources) RestoreResource(_ context.Context, org, rid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, resourceCall{"restore", iam.Resource{ID: rid, OrganizationID: org}})
	return nil
}

type fakeTeams map[string]bool

func (t fakeTeams) IsActiveTeam(_ context.Context, org, teamID string) (bool, error) {
	return t[org+"/"+teamID], nil
}

type fakeAudit struct {
	mu     sync.Mutex
	events []audit.Event
	err    error
}

func (a *fakeAudit) Record(_ context.Context, e audit.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, e)
	return nil
}

func (a *fakeAudit) types() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.events))
	for i, e := range a.events {
		out[i] = e.Type
	}
	return out
}

func (a *fakeAudit) last() audit.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.events[len(a.events)-1]
}

// fakeLocks maps "org/workspace" (workspace may be empty) to locked keys.
type fakeLocks map[string]map[string]Level

func (l fakeLocks) LockedKeys(_ context.Context, org, wsID string) (map[string]Level, error) {
	out := map[string]Level{}
	for k, v := range l[org+"/"+wsID] {
		out[k] = v
	}
	// An Organization lock wins over a Workspace lock.
	for k, v := range l[org+"/"] {
		out[k] = v
	}
	return out, nil
}

type fakeDependent struct {
	deps []Dependency
	err  error
}

func (d fakeDependent) ActiveDependents(context.Context, ArchiveTarget) ([]Dependency, error) {
	return d.deps, d.err
}

// fixture is a Service wired to fakes, with one Organization and Team.
type fixture struct {
	svc         *Service
	repo        *fakeRepo
	authz       *fakeAuthz
	resources   *fakeResources
	audit       *fakeAudit
	locks       fakeLocks
	definitions *Definitions
	now         time.Time
}

const (
	testOrg  = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testTeam = "tem_1"
)

var (
	alice = identity.Actor{PrincipalID: "prn_alice", Kind: identity.ActorUser}
	agent = identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorAgentSession, SessionID: "ses_1"}
)

// fixedNow is the time the domain clock reports in tests that use a fixture.
var fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fixClock makes the domain clock report at until the test ends. Tests that
// call it must not run in parallel, because the clock is shared.
func fixClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := clock
	clock = func() time.Time { return at }
	t.Cleanup(func() { clock = previous })
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fixClock(t, fixedNow)
	f := &fixture{
		repo:        newFakeRepo(),
		authz:       &fakeAuthz{},
		resources:   &fakeResources{},
		audit:       &fakeAudit{},
		locks:       fakeLocks{},
		definitions: NewDefinitions(),
		now:         fixedNow,
	}
	svc, err := NewService(Config{
		Repository:  f.repo,
		Transactor:  fakeTx{},
		Authorizer:  f.authz,
		Resources:   f.resources,
		Teams:       fakeTeams{testOrg + "/" + testTeam: true, testOrg + "/tem_2": true},
		Audit:       f.audit,
		Locks:       f.locks,
		Definitions: f.definitions,
	})
	require.NoError(t, err)
	f.svc = svc
	return f
}

// withDependents replaces the Service's dependents.
func (f *fixture) withDependents(deps ...Dependent) {
	f.svc.dependents = deps
}

func (f *fixture) defaultWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w, err := f.svc.ProvisionOrganization(context.Background(), testOrg)
	require.NoError(t, err)
	return w
}

func (f *fixture) workspace(t *testing.T, slug string) *Workspace {
	t.Helper()
	w, err := f.svc.CreateWorkspace(context.Background(), alice, CreateWorkspaceInput{OrganizationID: testOrg, Slug: slug, DisplayName: slug})
	require.NoError(t, err)
	return w
}

func (f *fixture) project(t *testing.T, ws *Workspace, slug string) *Project {
	t.Helper()
	p, err := f.svc.CreateProject(context.Background(), alice, CreateProjectInput{
		OrganizationID: testOrg, WorkspaceID: ws.ID(), OwnerTeamID: testTeam, Slug: slug, DisplayName: slug,
	})
	require.NoError(t, err)
	return p
}

var errBoom = errors.New("boom")

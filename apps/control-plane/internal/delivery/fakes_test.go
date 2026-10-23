package delivery

// Fakes and fixtures for the service tests. Package-level names carry the
// fake or svc prefix so they do not collide with the domain tests' helpers in
// the same package.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// fakeTxKey marks a context as inside a fake transaction.
type fakeTxKey struct{}

func fakeInTx(ctx context.Context) bool { return ctx.Value(fakeTxKey{}) != nil }

// fakeParticipant is a fake whose writes a fakeTx rolls back: checkpoint
// returns a function that restores the state it had.
type fakeParticipant interface {
	checkpoint() func()
}

// fakeTx runs fn and, if it returns an error, restores every participant to
// the state it had before, as a database rollback would. A call inside a
// transaction joins it.
type fakeTx struct {
	parts     []fakeParticipant
	calls     int
	rollbacks int
}

func (t *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	if fakeInTx(ctx) {
		return fn(ctx)
	}
	t.calls++
	restores := make([]func(), len(t.parts))
	for i, p := range t.parts {
		restores[i] = p.checkpoint()
	}
	err := fn(context.WithValue(ctx, fakeTxKey{}, true))
	if err != nil {
		t.rollbacks++
		for _, r := range restores {
			r()
		}
	}
	return err
}

// fakeState is everything a fakeStore holds, by value.
type fakeState struct {
	environments map[string]EnvironmentSnapshot
	lifecycles   map[string]LifecycleSnapshot
	targets      map[string]TargetSnapshot
	registries   map[string]RegistrySnapshot
	applications map[string]ApplicationSnapshot
	artifacts    map[string]ArtifactSnapshot
	channels     map[string]ChannelSnapshot
	placements   map[string]PlacementSnapshot
}

func (s fakeState) clone() fakeState {
	return fakeState{
		environments: maps.Clone(s.environments),
		lifecycles:   maps.Clone(s.lifecycles),
		targets:      maps.Clone(s.targets),
		registries:   maps.Clone(s.registries),
		applications: maps.Clone(s.applications),
		artifacts:    maps.Clone(s.artifacts),
		channels:     maps.Clone(s.channels),
		placements:   maps.Clone(s.placements),
	}
}

// fakeStore is an in-memory implementation of every delivery repository,
// with the uniqueness rules the migrations enforce. Like the PostgreSQL
// repositories, it stores snapshots and rebuilds objects with the New…
// constructors, so callers never share an object with it. Snapshots are
// replaced whole, never changed in place, so a shallow map copy is a
// consistent checkpoint.
type fakeStore struct {
	mu sync.Mutex
	fakeState
	// inUse names placements ("app/target") that Deployments refer to.
	inUse map[string]bool
	// fail makes the named method return the error.
	fail map[string]error
	// writes counts successful creates, updates, and deletes, including
	// ones later rolled back.
	writes int
}

var (
	_ EnvironmentRepository = (*fakeStore)(nil)
	_ LifecycleRepository   = (*fakeStore)(nil)
	_ TargetRepository      = (*fakeStore)(nil)
	_ RegistryRepository    = (*fakeStore)(nil)
	_ ApplicationRepository = (*fakeStore)(nil)
)

func newFakeStore() *fakeStore {
	return &fakeStore{
		fakeState: fakeState{
			environments: map[string]EnvironmentSnapshot{},
			lifecycles:   map[string]LifecycleSnapshot{},
			targets:      map[string]TargetSnapshot{},
			registries:   map[string]RegistrySnapshot{},
			applications: map[string]ApplicationSnapshot{},
			artifacts:    map[string]ArtifactSnapshot{},
			channels:     map[string]ChannelSnapshot{},
			placements:   map[string]PlacementSnapshot{},
		},
		inUse: map[string]bool{},
		fail:  map[string]error{},
	}
}

func (r *fakeStore) checkpoint() func() {
	r.mu.Lock()
	saved := r.fakeState.clone()
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.fakeState = saved
	}
}

// state returns a copy of everything stored, to compare before and after.
func (r *fakeStore) state() fakeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeState.clone()
}

func (r *fakeStore) failed(method string) error {
	return r.fail[method]
}

// requireTx is what FOR UPDATE needs: a transaction.
func fakeRequireTx(ctx context.Context, method string) error {
	if !fakeInTx(ctx) {
		return fmt.Errorf("%s called outside a transaction", method)
	}
	return nil
}

func fakeMust[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("fake store: rebuild: %v", err))
	}
	return v
}

func fakePage[T any](items []T) *pagination.Result[T] {
	return pagination.NewResult(items, len(items), false, false, nil, nil)
}

// fakeSorted returns the values ordered by creation time, then ID.
func fakeSorted[S any](m map[string]S, keep func(S) bool, created func(S) (time.Time, string)) []S {
	var out []S
	for _, s := range m {
		if keep(s) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b S) int {
		ta, ia := created(a)
		tb, ib := created(b)
		if c := ta.Compare(tb); c != 0 {
			return c
		}
		if ia < ib {
			return -1
		}
		if ia > ib {
			return 1
		}
		return 0
	})
	return out
}

// Rebuilding, as the PostgreSQL mappers do.

func fakeWorkspace(s workspace.WorkspaceSnapshot) *workspace.Workspace {
	return fakeMust(workspace.NewWorkspace(workspace.NewWorkspaceProps{
		OrganizationID:       s.OrganizationID,
		Slug:                 s.Slug,
		DisplayName:          s.DisplayName,
		Description:          s.Description,
		IsDefault:            s.IsDefault,
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
		ArchivedAt:           s.ArchivedAt,
	}, &s.ID))
}

func fakeProject(s workspace.ProjectSnapshot) *workspace.Project {
	return fakeMust(workspace.NewProject(workspace.NewProjectProps{
		OrganizationID:       s.OrganizationID,
		WorkspaceID:          s.WorkspaceID,
		OwnerTeamID:          s.OwnerTeamID,
		Slug:                 s.Slug,
		DisplayName:          s.DisplayName,
		Description:          s.Description,
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
		ArchivedAt:           s.ArchivedAt,
	}, &s.ID))
}

func fakeEnvironment(s EnvironmentSnapshot) *Environment {
	return fakeMust(NewEnvironment(NewEnvironmentProps{
		OrganizationID:   s.OrganizationID,
		WorkspaceID:      s.WorkspaceID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		Tier:             s.Tier,
		ExecutionEnabled: s.ExecutionEnabled,
		CreatedAt:        &s.CreatedAt,
		UpdatedAt:        &s.UpdatedAt,
		DeletedAt:        s.DeletedAt,
	}, &s.ID))
}

func fakeLifecycle(s LifecycleSnapshot) *Lifecycle {
	phases := make([]NewLifecyclePhaseProps, len(s.Phases))
	for i, p := range s.Phases {
		phases[i] = NewLifecyclePhaseProps{
			ID:              &p.ID,
			Name:            p.Name,
			Optional:        p.Optional,
			MinEnvironments: p.MinEnvironments,
			EnvironmentIDs:  p.EnvironmentIDs,
			CreatedAt:       &p.CreatedAt,
			UpdatedAt:       &p.UpdatedAt,
		}
	}
	return fakeMust(NewLifecycle(NewLifecycleProps{
		OrganizationID: s.OrganizationID,
		WorkspaceID:    s.WorkspaceID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Description:    s.Description,
		Phases:         phases,
		CreatedAt:      &s.CreatedAt,
		UpdatedAt:      &s.UpdatedAt,
		DeletedAt:      s.DeletedAt,
	}, &s.ID))
}

func fakeTarget(s TargetSnapshot) *Target {
	return fakeMust(NewTarget(NewTargetProps{
		OrganizationID:   s.OrganizationID,
		EnvironmentID:    s.EnvironmentID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		Runtime:          s.Runtime,
		Locator:          s.Locator,
		AgentPrincipalID: s.AgentPrincipalID,
		LastReportedAt:   s.LastReportedAt,
		CreatedAt:        &s.CreatedAt,
		UpdatedAt:        &s.UpdatedAt,
		DeletedAt:        s.DeletedAt,
	}, &s.ID))
}

func fakeRegistry(s RegistrySnapshot) *Registry {
	return fakeMust(NewRegistry(NewRegistryProps{
		OrganizationID: s.OrganizationID,
		WorkspaceID:    s.WorkspaceID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Kind:           s.Kind,
		URL:            s.URL,
		CredentialRef:  s.CredentialRef,
		LookupFrom:     s.LookupFrom,
		LookupPool:     s.LookupPool,
		CreatedAt:      &s.CreatedAt,
		UpdatedAt:      &s.UpdatedAt,
		DeletedAt:      s.DeletedAt,
	}, &s.ID))
}

func fakeApplication(s ApplicationSnapshot) *Application {
	return fakeMust(NewApplication(NewApplicationProps{
		OrganizationID:   s.OrganizationID,
		WorkspaceID:      s.WorkspaceID,
		ProjectID:        s.ProjectID,
		OwnerTeamID:      s.OwnerTeamID,
		Slug:             s.Slug,
		DisplayName:      s.DisplayName,
		SourceID:         s.SourceID,
		WorkloadSelector: s.WorkloadSelector,
		CreatedAt:        &s.CreatedAt,
		UpdatedAt:        &s.UpdatedAt,
		ArchivedAt:       s.ArchivedAt,
	}, &s.ID))
}

func fakeArtifact(s ArtifactSnapshot) *Artifact {
	return fakeMust(NewArtifact(NewArtifactProps{
		OrganizationID: s.OrganizationID,
		ApplicationID:  s.ApplicationID,
		Name:           s.Name,
		ArtifactKind:   s.ArtifactKind,
		ArtifactName:   s.ArtifactName,
		RegistryID:     s.RegistryID,
		Required:       s.Required,
		Source:         s.Source,
		CreatedAt:      &s.CreatedAt,
		UpdatedAt:      &s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}, &s.ID))
}

func fakeChannel(s ChannelSnapshot) *Channel {
	rules := make([]NewChannelRuleProps, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = NewChannelRuleProps{
			ID:         &r.ID,
			Kind:       r.Kind,
			ArtifactID: r.ArtifactID,
			Config:     r.Config,
			CreatedAt:  &r.CreatedAt,
			UpdatedAt:  &r.UpdatedAt,
		}
	}
	return fakeMust(NewChannel(NewChannelProps{
		OrganizationID: s.OrganizationID,
		ApplicationID:  s.ApplicationID,
		LifecycleID:    s.LifecycleID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Description:    s.Description,
		IsDefault:      s.IsDefault,
		Rules:          rules,
		CreatedAt:      &s.CreatedAt,
		UpdatedAt:      &s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}, &s.ID))
}

func fakePlacement(s PlacementSnapshot) *Placement {
	return fakeMust(NewPlacement(NewPlacementProps{
		OrganizationID:      s.OrganizationID,
		ApplicationID:       s.ApplicationID,
		TargetID:            s.TargetID,
		Source:              s.Source,
		ApplyConfig:         s.ApplyConfig,
		CurrentDeploymentID: s.CurrentDeploymentID,
		CurrentReleaseID:    s.CurrentReleaseID,
		CreatedAt:           &s.CreatedAt,
		UpdatedAt:           &s.UpdatedAt,
	}))
}

// Environments.

func (r *fakeStore) checkEnvironment(s EnvironmentSnapshot) error {
	for _, o := range r.environments {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.WorkspaceID == s.WorkspaceID &&
			o.DeletedAt == nil && s.DeletedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateEnvironment(_ context.Context, e *Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateEnvironment"); err != nil {
		return err
	}
	s := e.Snapshot()
	if _, ok := r.environments[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkEnvironment(s); err != nil {
		return err
	}
	r.environments[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetEnvironment(_ context.Context, org, envID string) (*Environment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.environments[envID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeEnvironment(s), nil
}

func (r *fakeStore) GetEnvironmentForUpdate(ctx context.Context, org, envID string) (*Environment, error) {
	if err := fakeRequireTx(ctx, "GetEnvironmentForUpdate"); err != nil {
		return nil, err
	}
	return r.GetEnvironment(ctx, org, envID)
}

func (r *fakeStore) GetEnvironmentBySlug(_ context.Context, org, wsID, slug string) (*Environment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.environments {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.Slug == slug && s.DeletedAt == nil {
			return fakeEnvironment(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) GetEnvironments(_ context.Context, org string, ids []string) ([]*Environment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Environment
	for _, envID := range ids {
		if s, ok := r.environments[envID]; ok && s.OrganizationID == org {
			out = append(out, fakeEnvironment(s))
		}
	}
	return out, nil
}

func (r *fakeStore) ListEnvironments(_ context.Context, q ListEnvironmentsQuery) (*pagination.Result[*Environment], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Environment
	for _, s := range fakeSorted(r.environments,
		func(s EnvironmentSnapshot) bool {
			return s.OrganizationID == q.OrganizationID && s.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || s.DeletedAt == nil)
		},
		func(s EnvironmentSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeEnvironment(s))
	}
	return fakePage(out), nil
}

func (r *fakeStore) UpdateEnvironment(_ context.Context, e *Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateEnvironment"); err != nil {
		return err
	}
	s := e.Snapshot()
	if o, ok := r.environments[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkEnvironment(s); err != nil {
		return err
	}
	r.environments[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveEnvironments(_ context.Context, org, wsID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.environments {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

// Lifecycles.

func (r *fakeStore) checkLifecycle(s LifecycleSnapshot) error {
	for _, o := range r.lifecycles {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.WorkspaceID == s.WorkspaceID &&
			o.DeletedAt == nil && s.DeletedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateLifecycle(_ context.Context, l *Lifecycle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateLifecycle"); err != nil {
		return err
	}
	s := l.Snapshot()
	if _, ok := r.lifecycles[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkLifecycle(s); err != nil {
		return err
	}
	r.lifecycles[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetLifecycle(_ context.Context, org, lfcID string) (*Lifecycle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.lifecycles[lfcID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeLifecycle(s), nil
}

func (r *fakeStore) GetLifecycleForUpdate(ctx context.Context, org, lfcID string) (*Lifecycle, error) {
	if err := fakeRequireTx(ctx, "GetLifecycleForUpdate"); err != nil {
		return nil, err
	}
	return r.GetLifecycle(ctx, org, lfcID)
}

func (r *fakeStore) GetLifecycleBySlug(_ context.Context, org, wsID, slug string) (*Lifecycle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.lifecycles {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.Slug == slug && s.DeletedAt == nil {
			return fakeLifecycle(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) ListLifecycles(_ context.Context, q ListLifecyclesQuery) (*pagination.Result[*Lifecycle], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Lifecycle
	for _, s := range fakeSorted(r.lifecycles,
		func(s LifecycleSnapshot) bool {
			return s.OrganizationID == q.OrganizationID && s.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || s.DeletedAt == nil)
		},
		func(s LifecycleSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeLifecycle(s))
	}
	return fakePage(out), nil
}

func (r *fakeStore) UpdateLifecycle(_ context.Context, l *Lifecycle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateLifecycle"); err != nil {
		return err
	}
	s := l.Snapshot()
	if o, ok := r.lifecycles[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkLifecycle(s); err != nil {
		return err
	}
	r.lifecycles[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveLifecycles(_ context.Context, org, wsID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.lifecycles {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

func (r *fakeStore) CountActiveLifecyclesWithEnvironment(_ context.Context, org, envID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.lifecycles {
		if s.OrganizationID != org || s.DeletedAt != nil {
			continue
		}
		for _, p := range s.Phases {
			if slices.Contains(p.EnvironmentIDs, envID) {
				n++
				break
			}
		}
	}
	return n, nil
}

// Targets.

func (r *fakeStore) checkTarget(s TargetSnapshot) error {
	if e, ok := r.environments[s.EnvironmentID]; !ok || e.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: environment %s", ErrNotFound, s.EnvironmentID)
	}
	for _, o := range r.targets {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.EnvironmentID == s.EnvironmentID &&
			o.DeletedAt == nil && s.DeletedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateTarget(_ context.Context, t *Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateTarget"); err != nil {
		return err
	}
	s := t.Snapshot()
	if _, ok := r.targets[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkTarget(s); err != nil {
		return err
	}
	r.targets[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetTarget(_ context.Context, org, tgtID string) (*Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.targets[tgtID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeTarget(s), nil
}

func (r *fakeStore) GetTargetBySlug(_ context.Context, org, envID, slug string) (*Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.targets {
		if s.OrganizationID == org && s.EnvironmentID == envID && s.Slug == slug && s.DeletedAt == nil {
			return fakeTarget(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) ListTargets(_ context.Context, q ListTargetsQuery) (*pagination.Result[*Target], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Target
	for _, s := range fakeSorted(r.targets,
		func(s TargetSnapshot) bool {
			return s.OrganizationID == q.OrganizationID && s.EnvironmentID == q.EnvironmentID && (q.IncludeArchived || s.DeletedAt == nil)
		},
		func(s TargetSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeTarget(s))
	}
	return fakePage(out), nil
}

func (r *fakeStore) UpdateTarget(_ context.Context, t *Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateTarget"); err != nil {
		return err
	}
	s := t.Snapshot()
	if o, ok := r.targets[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkTarget(s); err != nil {
		return err
	}
	r.targets[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveTargets(_ context.Context, org, envID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.targets {
		if s.OrganizationID == org && s.EnvironmentID == envID && s.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

// Registries.

func (r *fakeStore) checkRegistry(s RegistrySnapshot) error {
	for _, o := range r.registries {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.WorkspaceID == s.WorkspaceID &&
			o.DeletedAt == nil && s.DeletedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateRegistry(_ context.Context, reg *Registry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateRegistry"); err != nil {
		return err
	}
	s := reg.Snapshot()
	if _, ok := r.registries[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkRegistry(s); err != nil {
		return err
	}
	r.registries[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetRegistry(_ context.Context, org, regID string) (*Registry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.registries[regID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeRegistry(s), nil
}

func (r *fakeStore) GetRegistryBySlug(_ context.Context, org, wsID, slug string) (*Registry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.registries {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.Slug == slug && s.DeletedAt == nil {
			return fakeRegistry(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) ListRegistries(_ context.Context, q ListRegistriesQuery) (*pagination.Result[*Registry], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Registry
	for _, s := range fakeSorted(r.registries,
		func(s RegistrySnapshot) bool {
			return s.OrganizationID == q.OrganizationID && s.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || s.DeletedAt == nil)
		},
		func(s RegistrySnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeRegistry(s))
	}
	return fakePage(out), nil
}

func (r *fakeStore) UpdateRegistry(_ context.Context, reg *Registry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateRegistry"); err != nil {
		return err
	}
	s := reg.Snapshot()
	if o, ok := r.registries[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkRegistry(s); err != nil {
		return err
	}
	r.registries[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveRegistries(_ context.Context, org, wsID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.registries {
		if s.OrganizationID == org && s.WorkspaceID == wsID && s.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

// Applications.

func (r *fakeStore) checkApplication(s ApplicationSnapshot) error {
	for _, o := range r.applications {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.ProjectID == s.ProjectID &&
			o.ArchivedAt == nil && s.ArchivedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateApplication(_ context.Context, a *Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateApplication"); err != nil {
		return err
	}
	s := a.Snapshot()
	if _, ok := r.applications[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkApplication(s); err != nil {
		return err
	}
	r.applications[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetApplication(_ context.Context, org, appID string) (*Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.applications[appID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeApplication(s), nil
}

func (r *fakeStore) GetApplicationForUpdate(ctx context.Context, org, appID string) (*Application, error) {
	if err := fakeRequireTx(ctx, "GetApplicationForUpdate"); err != nil {
		return nil, err
	}
	return r.GetApplication(ctx, org, appID)
}

func (r *fakeStore) GetApplicationBySlug(_ context.Context, org, prjID, slug string) (*Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.applications {
		if s.OrganizationID == org && s.ProjectID == prjID && s.Slug == slug && s.ArchivedAt == nil {
			return fakeApplication(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) ListApplications(_ context.Context, q ListApplicationsQuery) (*pagination.Result[*Application], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Application
	for _, s := range fakeSorted(r.applications,
		func(s ApplicationSnapshot) bool {
			return s.OrganizationID == q.OrganizationID &&
				(q.ProjectID == "" || s.ProjectID == q.ProjectID) &&
				(q.WorkspaceID == "" || s.WorkspaceID == q.WorkspaceID) &&
				(q.OwnerTeamID == "" || s.OwnerTeamID == q.OwnerTeamID) &&
				(q.IncludeArchived || s.ArchivedAt == nil)
		},
		func(s ApplicationSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeApplication(s))
	}
	return fakePage(out), nil
}

func (r *fakeStore) UpdateApplication(_ context.Context, a *Application) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateApplication"); err != nil {
		return err
	}
	s := a.Snapshot()
	if o, ok := r.applications[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkApplication(s); err != nil {
		return err
	}
	r.applications[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveApplications(_ context.Context, org, prjID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.applications {
		if s.OrganizationID == org && s.ProjectID == prjID && s.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// Artifacts.

func (r *fakeStore) checkArtifact(s ArtifactSnapshot) error {
	if a, ok := r.applications[s.ApplicationID]; !ok || a.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: application %s", ErrNotFound, s.ApplicationID)
	}
	for _, o := range r.artifacts {
		if o.ID != s.ID && o.OrganizationID == s.OrganizationID && o.ApplicationID == s.ApplicationID &&
			o.ArchivedAt == nil && s.ArchivedAt == nil && o.Name == s.Name {
			return ErrSlugTaken
		}
	}
	return nil
}

func (r *fakeStore) CreateArtifact(_ context.Context, a *Artifact) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateArtifact"); err != nil {
		return err
	}
	s := a.Snapshot()
	if _, ok := r.artifacts[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkArtifact(s); err != nil {
		return err
	}
	r.artifacts[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetArtifact(_ context.Context, org, artID string) (*Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.artifacts[artID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeArtifact(s), nil
}

func (r *fakeStore) ListArtifacts(_ context.Context, org, appID string, includeArchived bool) ([]*Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Artifact
	for _, s := range fakeSorted(r.artifacts,
		func(s ArtifactSnapshot) bool {
			return s.OrganizationID == org && s.ApplicationID == appID && (includeArchived || s.ArchivedAt == nil)
		},
		func(s ArtifactSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeArtifact(s))
	}
	return out, nil
}

func (r *fakeStore) UpdateArtifact(_ context.Context, a *Artifact) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateArtifact"); err != nil {
		return err
	}
	s := a.Snapshot()
	if o, ok := r.artifacts[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkArtifact(s); err != nil {
		return err
	}
	r.artifacts[s.ID] = s
	r.writes++
	return nil
}

// Channels.

func (r *fakeStore) checkChannel(s ChannelSnapshot) error {
	if a, ok := r.applications[s.ApplicationID]; !ok || a.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: application %s", ErrNotFound, s.ApplicationID)
	}
	if l, ok := r.lifecycles[s.LifecycleID]; !ok || l.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: lifecycle %s", ErrNotFound, s.LifecycleID)
	}
	for _, o := range r.channels {
		if o.ID == s.ID || o.OrganizationID != s.OrganizationID || o.ApplicationID != s.ApplicationID {
			continue
		}
		if o.ArchivedAt == nil && s.ArchivedAt == nil && o.Slug == s.Slug {
			return ErrSlugTaken
		}
		if o.IsDefault && s.IsDefault {
			return ErrConflict
		}
	}
	return nil
}

func (r *fakeStore) CreateChannel(_ context.Context, c *Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreateChannel"); err != nil {
		return err
	}
	s := c.Snapshot()
	if _, ok := r.channels[s.ID]; ok {
		return ErrAlreadyExists
	}
	if err := r.checkChannel(s); err != nil {
		return err
	}
	r.channels[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetChannel(_ context.Context, org, chnID string) (*Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.channels[chnID]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakeChannel(s), nil
}

func (r *fakeStore) GetDefaultChannel(_ context.Context, org, appID string) (*Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.channels {
		if s.OrganizationID == org && s.ApplicationID == appID && s.IsDefault {
			return fakeChannel(s), nil
		}
	}
	return nil, ErrNotFound
}

func (r *fakeStore) ListChannels(_ context.Context, org, appID string, includeArchived bool) ([]*Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Channel
	for _, s := range fakeSorted(r.channels,
		func(s ChannelSnapshot) bool {
			return s.OrganizationID == org && s.ApplicationID == appID && (includeArchived || s.ArchivedAt == nil)
		},
		func(s ChannelSnapshot) (time.Time, string) { return s.CreatedAt, s.ID }) {
		out = append(out, fakeChannel(s))
	}
	return out, nil
}

func (r *fakeStore) UpdateChannel(_ context.Context, c *Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdateChannel"); err != nil {
		return err
	}
	s := c.Snapshot()
	if o, ok := r.channels[s.ID]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	if err := r.checkChannel(s); err != nil {
		return err
	}
	r.channels[s.ID] = s
	r.writes++
	return nil
}

func (r *fakeStore) CountActiveChannelsWithLifecycle(_ context.Context, org, lfcID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.channels {
		app, ok := r.applications[s.ApplicationID]
		if s.OrganizationID == org && s.LifecycleID == lfcID && s.ArchivedAt == nil && ok && app.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// Placements.

func fakePlacementKey(appID, tgtID string) string { return appID + "/" + tgtID }

func (r *fakeStore) CreatePlacement(_ context.Context, p *Placement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("CreatePlacement"); err != nil {
		return err
	}
	s := p.Snapshot()
	if a, ok := r.applications[s.ApplicationID]; !ok || a.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: application %s", ErrNotFound, s.ApplicationID)
	}
	if t, ok := r.targets[s.TargetID]; !ok || t.OrganizationID != s.OrganizationID {
		return fmt.Errorf("%w: foreign key: target %s", ErrNotFound, s.TargetID)
	}
	k := fakePlacementKey(s.ApplicationID, s.TargetID)
	if _, ok := r.placements[k]; ok {
		return ErrAlreadyExists
	}
	r.placements[k] = s
	r.writes++
	return nil
}

func (r *fakeStore) GetPlacement(_ context.Context, org, appID, tgtID string) (*Placement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.placements[fakePlacementKey(appID, tgtID)]
	if !ok || s.OrganizationID != org {
		return nil, ErrNotFound
	}
	return fakePlacement(s), nil
}

func (r *fakeStore) ListPlacements(_ context.Context, org, appID string) ([]*Placement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Placement
	for _, s := range fakeSorted(r.placements,
		func(s PlacementSnapshot) bool { return s.OrganizationID == org && s.ApplicationID == appID },
		func(s PlacementSnapshot) (time.Time, string) { return s.CreatedAt, s.TargetID }) {
		out = append(out, fakePlacement(s))
	}
	return out, nil
}

func (r *fakeStore) UpdatePlacement(_ context.Context, p *Placement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("UpdatePlacement"); err != nil {
		return err
	}
	s := p.Snapshot()
	k := fakePlacementKey(s.ApplicationID, s.TargetID)
	if o, ok := r.placements[k]; !ok || o.OrganizationID != s.OrganizationID {
		return ErrNotFound
	}
	r.placements[k] = s
	r.writes++
	return nil
}

func (r *fakeStore) DeletePlacement(_ context.Context, org, appID, tgtID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failed("DeletePlacement"); err != nil {
		return err
	}
	k := fakePlacementKey(appID, tgtID)
	if o, ok := r.placements[k]; !ok || o.OrganizationID != org {
		return ErrNotFound
	}
	if r.inUse[k] {
		return fmt.Errorf("%w: deployments refer to application %s on target %s", ErrInUse, appID, tgtID)
	}
	delete(r.placements, k)
	r.writes++
	return nil
}

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

func (a *fakeAuthz) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = nil
	a.decide = nil
}

func (a *fakeAuthz) all() []authorization.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.requests)
}

func fakeDecide(o authorization.Outcome) func(authorization.Request) authorization.Decision {
	return func(authorization.Request) authorization.Decision {
		return authorization.Decision{ID: "dec_" + string(o), Outcome: o, Reason: "test"}
	}
}

type fakeResourceCall struct {
	op string
	iam.Resource
}

// fakeResources records IAM resource calls; a fakeTx rolls them back.
type fakeResources struct {
	mu    sync.Mutex
	calls []fakeResourceCall
	err   error
}

func (f *fakeResources) checkpoint() func() {
	f.mu.Lock()
	n := len(f.calls)
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = f.calls[:n]
	}
}

func (f *fakeResources) add(c fakeResourceCall) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, c)
	return nil
}

func (f *fakeResources) RegisterResource(_ context.Context, r iam.Resource) error {
	return f.add(fakeResourceCall{"register", r})
}

func (f *fakeResources) ArchiveResource(_ context.Context, org, rid string) error {
	return f.add(fakeResourceCall{"archive", iam.Resource{ID: rid, OrganizationID: org}})
}

func (f *fakeResources) RestoreResource(_ context.Context, org, rid string) error {
	return f.add(fakeResourceCall{"restore", iam.Resource{ID: rid, OrganizationID: org}})
}

func (f *fakeResources) all() []fakeResourceCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// fakeTeams maps "org/team" to whether the Team is active.
type fakeTeams map[string]bool

func (t fakeTeams) IsActiveTeam(_ context.Context, org, teamID string) (bool, error) {
	return t[org+"/"+teamID], nil
}

// fakeAudit records audit events; a fakeTx rolls them back.
type fakeAudit struct {
	mu     sync.Mutex
	events []audit.Event
	err    error
}

func (a *fakeAudit) checkpoint() func() {
	a.mu.Lock()
	n := len(a.events)
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.events = a.events[:n]
	}
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

func (a *fakeAudit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.events)
}

func (a *fakeAudit) since(n int) []audit.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.events[n:])
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

// fakeWorkspaces is the workspace capability's lookups, over stored data.
type fakeWorkspaces struct {
	mu         sync.Mutex
	workspaces map[string]workspace.WorkspaceSnapshot
	projects   map[string]workspace.ProjectSnapshot
}

func newFakeWorkspaces() *fakeWorkspaces {
	return &fakeWorkspaces{workspaces: map[string]workspace.WorkspaceSnapshot{}, projects: map[string]workspace.ProjectSnapshot{}}
}

func (f *fakeWorkspaces) putWorkspace(w *workspace.Workspace) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaces[w.ID()] = w.Snapshot()
}

func (f *fakeWorkspaces) putProject(p *workspace.Project) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[p.ID()] = p.Snapshot()
}

func (f *fakeWorkspaces) LookupWorkspace(_ context.Context, org, wsID string) (*workspace.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.workspaces[wsID]
	if !ok || d.OrganizationID != org {
		return nil, workspace.ErrNotFound
	}
	return fakeWorkspace(d), nil
}

func (f *fakeWorkspaces) LookupProject(_ context.Context, org, prjID string) (*workspace.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.projects[prjID]
	if !ok || d.OrganizationID != org {
		return nil, workspace.ErrNotFound
	}
	return fakeProject(d), nil
}

// The fixture.

const (
	svcOrg       = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	svcTeam      = "tem_owner"
	svcOtherTeam = "tem_other"
	svcGoneTeam  = "tem_gone"
)

var (
	svcAlice = identity.Actor{PrincipalID: "prn_alice", Kind: identity.ActorUser}
	// svcAgent is a runtime agent's workload principal.
	svcAgent = identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorServiceAccount}

	// svcNow is the time the domain clock reports in service tests.
	svcNow = time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)

	svcCtx     = context.Background()
	svcErrBoom = errors.New("boom")
)

// svcFixture is a Service wired to fakes, with one active Workspace and a
// Project in it.
type svcFixture struct {
	svc        *Service
	store      *fakeStore
	tx         *fakeTx
	authz      *fakeAuthz
	resources  *fakeResources
	teams      fakeTeams
	audit      *fakeAudit
	workspaces *fakeWorkspaces
	ws         *workspace.Workspace
	project    *workspace.Project
}

func newSvcFixture(t *testing.T) *svcFixture {
	t.Helper()
	clock.Fix(t, svcNow)
	f := &svcFixture{
		store:      newFakeStore(),
		authz:      &fakeAuthz{},
		resources:  &fakeResources{},
		audit:      &fakeAudit{},
		teams:      fakeTeams{svcOrg + "/" + svcTeam: true, svcOrg + "/" + svcOtherTeam: true, svcOrg + "/" + svcGoneTeam: false},
		workspaces: newFakeWorkspaces(),
	}
	f.tx = &fakeTx{parts: []fakeParticipant{f.store, f.audit, f.resources}}
	svc, err := NewService(f.config())
	require.NoError(t, err)
	f.svc = svc
	f.ws, f.project = f.addWorkspace(t, "platform", "checkout")
	return f
}

func (f *svcFixture) config() Config {
	return Config{
		Environments: f.store,
		Lifecycles:   f.store,
		Targets:      f.store,
		Registries:   f.store,
		Applications: f.store,
		Transactor:   f.tx,
		Authorizer:   f.authz,
		Resources:    f.resources,
		Teams:        f.teams,
		Audit:        f.audit,
		Workspaces:   f.workspaces,
	}
}

// addWorkspace stores an active Workspace with a Project owned by svcTeam.
func (f *svcFixture) addWorkspace(t *testing.T, wsSlug, projectSlug string) (*workspace.Workspace, *workspace.Project) {
	t.Helper()
	w, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: svcOrg, Slug: wsSlug, DisplayName: wsSlug}, nil)
	require.NoError(t, err)
	f.workspaces.putWorkspace(w)
	p, err := workspace.NewProject(workspace.NewProjectProps{OrganizationID: svcOrg, WorkspaceID: w.ID(), OwnerTeamID: svcTeam, Slug: projectSlug, DisplayName: projectSlug}, nil)
	require.NoError(t, err)
	f.workspaces.putProject(p)
	return w, p
}

func (f *svcFixture) archiveWorkspace(t *testing.T, w *workspace.Workspace) {
	t.Helper()
	require.NoError(t, w.Archive())
	f.workspaces.putWorkspace(w)
}

func (f *svcFixture) archiveProject(p *workspace.Project) {
	p.Archive()
	f.workspaces.putProject(p)
}

// marks returns what tests compare to show that nothing was persisted.
func (f *svcFixture) marks() (fakeState, int, int) {
	return f.store.state(), f.audit.count(), len(f.resources.all())
}

func (f *svcFixture) requireUnchanged(t *testing.T, state fakeState, events, resources int) {
	t.Helper()
	require.Equal(t, state, f.store.state(), "nothing is persisted")
	require.Equal(t, events, f.audit.count(), "nothing is audited")
	require.Len(t, f.resources.all(), resources, "no IAM resource changes")
}

func (f *svcFixture) envIn(t *testing.T, wsID, slug string) *Environment {
	t.Helper()
	e, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{
		OrganizationID: svcOrg, WorkspaceID: wsID, Slug: slug, DisplayName: slug, Tier: TierStaging,
	})
	require.NoError(t, err)
	return e
}

func (f *svcFixture) env(t *testing.T, slug string) *Environment {
	t.Helper()
	return f.envIn(t, f.ws.ID(), slug)
}

func svcLocator(namespace string) map[string]string {
	return map[string]string{LocatorCluster: "prod-1", LocatorNamespace: namespace}
}

func (f *svcFixture) target(t *testing.T, e *Environment, slug string) *Target {
	t.Helper()
	tg, err := f.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{
		OrganizationID: svcOrg, EnvironmentID: e.ID(), Slug: slug, DisplayName: slug,
		Runtime: RuntimeKubernetes, Locator: svcLocator(slug),
	})
	require.NoError(t, err)
	return tg
}

// lifecycleIn creates a Lifecycle with one phase per Environment.
func (f *svcFixture) lifecycleIn(t *testing.T, wsID, slug string, envs ...*Environment) *Lifecycle {
	t.Helper()
	phases := make([]LifecyclePhaseProps, len(envs))
	for i, e := range envs {
		phases[i] = LifecyclePhaseProps{Name: e.Slug(), EnvironmentIDs: []string{e.ID()}}
	}
	l, err := f.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{
		OrganizationID: svcOrg, WorkspaceID: wsID, Slug: slug, DisplayName: slug, Phases: phases,
	})
	require.NoError(t, err)
	return l
}

func (f *svcFixture) lifecycle(t *testing.T, slug string, envs ...*Environment) *Lifecycle {
	t.Helper()
	return f.lifecycleIn(t, f.ws.ID(), slug, envs...)
}

func (f *svcFixture) registryIn(t *testing.T, wsID, slug string) *Registry {
	t.Helper()
	r, err := f.svc.CreateRegistry(svcCtx, svcAlice, CreateRegistryInput{
		OrganizationID: svcOrg, WorkspaceID: wsID, Slug: slug, DisplayName: slug, Kind: RegistryOCI, URL: "https://ghcr.io",
	})
	require.NoError(t, err)
	return r
}

func (f *svcFixture) registry(t *testing.T, slug string) *Registry {
	t.Helper()
	return f.registryIn(t, f.ws.ID(), slug)
}

func (f *svcFixture) appIn(t *testing.T, p *workspace.Project, slug string, l *Lifecycle) (*Application, *Channel) {
	t.Helper()
	a, c, err := f.svc.CreateApplication(svcCtx, svcAlice, CreateApplicationInput{
		OrganizationID: svcOrg, ProjectID: p.ID(), Slug: slug, DisplayName: slug, DefaultLifecycleID: l.ID(),
	})
	require.NoError(t, err)
	return a, c
}

func (f *svcFixture) app(t *testing.T, slug string, l *Lifecycle) (*Application, *Channel) {
	t.Helper()
	return f.appIn(t, f.project, slug, l)
}

func (f *svcFixture) artifact(t *testing.T, a *Application, name string) *Artifact {
	t.Helper()
	art, err := f.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{
		OrganizationID: svcOrg, ApplicationID: a.ID(), Name: name, ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/" + name,
	})
	require.NoError(t, err)
	return art
}

func (f *svcFixture) channel(t *testing.T, a *Application, l *Lifecycle, slug string, rules ...ChannelRuleProps) *Channel {
	t.Helper()
	c, err := f.svc.CreateChannel(svcCtx, svcAlice, CreateChannelInput{
		OrganizationID: svcOrg, ApplicationID: a.ID(), LifecycleID: l.ID(), Slug: slug, DisplayName: slug, Rules: rules,
	})
	require.NoError(t, err)
	return c
}

func (f *svcFixture) placement(t *testing.T, a *Application, tg *Target, config *ApplyConfig) *Placement {
	t.Helper()
	p, err := f.svc.CreatePlacement(svcCtx, svcAlice, CreatePlacementInput{
		OrganizationID: svcOrg, ApplicationID: a.ID(), TargetID: tg.ID(), ApplyConfig: config,
	})
	require.NoError(t, err)
	return p
}

// svcGitRule is a valid rule, scoped to an artifact when artifactID is set.
func svcGitRule(artifactID *string) ChannelRuleProps {
	return ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: artifactID, Config: []byte(`{"patterns":["refs/heads/main"]}`)}
}

// svcWorld is a fixture with one of everything, for tests that run every
// operation.
type svcWorld struct {
	*svcFixture
	env       *Environment // in lifecycle, with target
	spareEnv  *Environment // in no lifecycle, without targets
	oldEnv    *Environment // archived
	target    *Target
	lifecycle *Lifecycle
	registry  *Registry
	app       *Application
	defaultCh *Channel
	artifact  *Artifact
	chart     *Artifact
	channel   *Channel // not the default, with a rule scoped to artifact
	placement *Placement
}

func newSvcWorld(t *testing.T) *svcWorld {
	t.Helper()
	f := newSvcFixture(t)
	w := &svcWorld{svcFixture: f}
	w.env = f.env(t, "staging")
	w.spareEnv = f.env(t, "qa")
	w.oldEnv = f.env(t, "old")
	_, err := f.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{w.oldEnv.ID()})
	require.NoError(t, err)
	w.target = f.target(t, w.env, "web")
	w.lifecycle = f.lifecycle(t, "mainline", w.env)
	w.registry = f.registry(t, "ghcr")
	w.app, w.defaultCh = f.app(t, "api", w.lifecycle)
	w.artifact = f.artifact(t, w.app, "api")
	w.chart = f.artifact(t, w.app, "chart")
	w.channel = f.channel(t, w.app, w.lifecycle, "hotfix", svcGitRule(new(w.artifact.ID())))
	w.placement = f.placement(t, w.app, w.target, &ApplyConfig{DefinitionArtifact: "chart"})
	f.authz.reset()
	return w
}

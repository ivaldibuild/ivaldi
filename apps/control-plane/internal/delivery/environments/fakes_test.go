package environments

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const org = "org_acme"

// state is everything the fakes store. A transaction restores it when its
// function fails, as a rolled-back database transaction would.
type state struct {
	environments map[string]delivery.EnvironmentSnapshot
	targets      map[string]delivery.TargetSnapshot
	clusters     map[string]delivery.ClusterSnapshot
	resources    map[string]iam.Resource
	archivedRes  map[string]bool
	audit        []audit.Event
	events       []events.Envelope
}

func (s state) clone() state {
	return state{
		environments: maps.Clone(s.environments),
		targets:      maps.Clone(s.targets),
		clusters:     maps.Clone(s.clusters),
		resources:    maps.Clone(s.resources),
		archivedRes:  maps.Clone(s.archivedRes),
		audit:        append([]audit.Event(nil), s.audit...),
		events:       append([]events.Envelope(nil), s.events...),
	}
}

// store fakes the repositories, the transactor, the resource registrar, and
// the Workspaces lookup. Its auditLog and outboxLog fake the recorders.
type store struct {
	state
	workspaces   map[string]*workspace.Workspace
	lifecycleUse map[string]int
	// scopedVariables counts the variables scoped to an Environment or Target.
	scopedVariables map[string]int
	// inTx and aborted fake PostgreSQL's rule that a transaction ignores
	// every command after a failed write, so a service that looks something
	// up in the transaction that just failed is caught.
	inTx    int
	aborted bool
}

var errTxAborted = errors.New("current transaction is aborted, commands ignored until end of transaction block")

// failWrite marks the transaction aborted, as PostgreSQL does on a failed
// write, and returns err.
func (s *store) failWrite(err error) error {
	if s.inTx > 0 {
		s.aborted = true
	}
	return err
}

// namespaceTaken fakes idx_targets_cluster_namespace.
func (s *store) namespaceTaken(t delivery.TargetSnapshot) bool {
	if t.DeletedAt != nil {
		return false
	}
	for _, o := range s.targets {
		if o.ID != t.ID && o.OrganizationID == t.OrganizationID && o.ClusterID == t.ClusterID &&
			o.Locator[delivery.LocatorNamespace] == t.Locator[delivery.LocatorNamespace] && o.DeletedAt == nil {
			return true
		}
	}
	return false
}

func newStore() *store {
	return &store{
		state: state{
			environments: map[string]delivery.EnvironmentSnapshot{},
			targets:      map[string]delivery.TargetSnapshot{},
			clusters:     map[string]delivery.ClusterSnapshot{},
			resources:    map[string]iam.Resource{},
			archivedRes:  map[string]bool{},
		},
		workspaces:      map[string]*workspace.Workspace{},
		lifecycleUse:    map[string]int{},
		scopedVariables: map[string]int{},
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
	s.inTx++
	defer func() {
		s.inTx--
		if s.inTx == 0 {
			s.aborted = false
		}
	}()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

// Environments.

func rebuildEnvironment(s delivery.EnvironmentSnapshot) *delivery.Environment {
	return must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID, Slug: s.Slug, DisplayName: s.DisplayName,
		Tier: s.Tier, ExecutionEnabled: s.ExecutionEnabled,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

func (s *store) CreateEnvironment(_ context.Context, e *delivery.Environment) error {
	snap := e.Snapshot()
	for _, o := range s.environments {
		if o.WorkspaceID == snap.WorkspaceID && o.Slug == snap.Slug && o.DeletedAt == nil {
			return delivery.ErrSlugTaken
		}
	}
	s.environments[snap.ID] = snap
	return nil
}

func (s *store) GetEnvironment(_ context.Context, organizationID, id string) (*delivery.Environment, error) {
	if s.aborted {
		return nil, errTxAborted
	}
	snap, ok := s.environments[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildEnvironment(snap), nil
}

func (s *store) GetEnvironmentForUpdate(ctx context.Context, organizationID, id string) (*delivery.Environment, error) {
	return s.GetEnvironment(ctx, organizationID, id)
}

func (s *store) GetEnvironmentBySlug(_ context.Context, organizationID, workspaceID, slug string) (*delivery.Environment, error) {
	for _, snap := range s.environments {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.Slug == slug && snap.DeletedAt == nil {
			return rebuildEnvironment(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetEnvironments(ctx context.Context, organizationID string, ids []string) ([]*delivery.Environment, error) {
	var out []*delivery.Environment
	for _, id := range ids {
		if e, err := s.GetEnvironment(ctx, organizationID, id); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *store) ListEnvironments(_ context.Context, q delivery.ListEnvironmentsQuery) (*pagination.Result[*delivery.Environment], error) {
	out := &pagination.Result[*delivery.Environment]{}
	for _, snap := range s.environments {
		if snap.OrganizationID == q.OrganizationID && snap.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || snap.DeletedAt == nil) {
			out.Items = append(out.Items, rebuildEnvironment(snap))
		}
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) ListActiveEnvironments(_ context.Context, organizationID, workspaceID string) ([]*delivery.Environment, error) {
	var out []*delivery.Environment
	for _, snap := range s.environments {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.DeletedAt == nil {
			out = append(out, rebuildEnvironment(snap))
		}
	}
	return out, nil
}

func (s *store) UpdateEnvironment(_ context.Context, e *delivery.Environment) error {
	if _, ok := s.environments[e.ID()]; !ok {
		return delivery.ErrNotFound
	}
	s.environments[e.ID()] = e.Snapshot()
	return nil
}

func (s *store) CountActiveEnvironments(_ context.Context, organizationID, workspaceID string) (int, error) {
	n := 0
	for _, snap := range s.environments {
		if snap.OrganizationID == organizationID && snap.WorkspaceID == workspaceID && snap.DeletedAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *store) CountActiveLifecyclesWithEnvironment(_ context.Context, _, environmentID string) (int, error) {
	return s.lifecycleUse[environmentID], nil
}

// Targets.

func rebuildTarget(s delivery.TargetSnapshot) *delivery.Target {
	return must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: s.OrganizationID, EnvironmentID: s.EnvironmentID, ClusterID: s.ClusterID,
		Slug: s.Slug, DisplayName: s.DisplayName,
		Runtime: s.Runtime, Locator: s.Locator, CanObserve: s.CanObserve, CanExecute: s.CanExecute,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

func rebuildCluster(s delivery.ClusterSnapshot) *delivery.Cluster {
	return must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: s.OrganizationID, Slug: s.Slug, DisplayName: s.DisplayName, Runtime: s.Runtime,
		AgentPrincipalID: s.AgentPrincipalID, AgentCapabilities: s.AgentCapabilities,
		AgentLastFailure: s.AgentLastFailure, LastReportedAt: s.LastReportedAt,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

func (s *store) GetClusterBySlug(_ context.Context, organizationID, slug string) (*delivery.Cluster, error) {
	for _, snap := range s.clusters {
		if snap.OrganizationID == organizationID && snap.Slug == slug && snap.DeletedAt == nil {
			return rebuildCluster(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetClusterForUpdate(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	snap, ok := s.clusters[clusterID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildCluster(snap), nil
}

func (s *store) CreateTarget(_ context.Context, t *delivery.Target) error {
	snap := t.Snapshot()
	if s.namespaceTaken(snap) {
		return s.failWrite(delivery.ErrNamespaceTaken)
	}
	s.targets[snap.ID] = snap
	return nil
}

func (s *store) GetTarget(_ context.Context, organizationID, id string) (*delivery.Target, error) {
	if s.aborted {
		return nil, errTxAborted
	}
	snap, ok := s.targets[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildTarget(snap), nil
}

func (s *store) GetTargetForUpdate(ctx context.Context, organizationID, id string) (*delivery.Target, error) {
	return s.GetTarget(ctx, organizationID, id)
}

func (s *store) GetTargetBySlug(_ context.Context, organizationID, environmentID, slug string) (*delivery.Target, error) {
	for _, snap := range s.targets {
		if snap.OrganizationID == organizationID && snap.EnvironmentID == environmentID && snap.Slug == slug && snap.DeletedAt == nil {
			return rebuildTarget(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetTargetByNamespace(_ context.Context, organizationID, clusterID, namespace string) (*delivery.Target, error) {
	if s.aborted {
		return nil, errTxAborted
	}
	for _, snap := range s.targets {
		if snap.OrganizationID == organizationID && snap.ClusterID == clusterID &&
			snap.Locator[delivery.LocatorNamespace] == namespace && snap.DeletedAt == nil {
			return rebuildTarget(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListTargets(_ context.Context, q delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	out := &pagination.Result[*delivery.Target]{}
	for _, snap := range s.targets {
		if snap.OrganizationID == q.OrganizationID && snap.EnvironmentID == q.EnvironmentID && (q.IncludeArchived || snap.DeletedAt == nil) {
			out.Items = append(out.Items, rebuildTarget(snap))
		}
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) UpdateTarget(_ context.Context, t *delivery.Target) error {
	if _, ok := s.targets[t.ID()]; !ok {
		return delivery.ErrNotFound
	}
	if s.namespaceTaken(t.Snapshot()) {
		return s.failWrite(delivery.ErrNamespaceTaken)
	}
	s.targets[t.ID()] = t.Snapshot()
	return nil
}

func (s *store) CountActiveTargets(_ context.Context, organizationID, environmentID string) (int, error) {
	n := 0
	for _, snap := range s.targets {
		if snap.OrganizationID == organizationID && snap.EnvironmentID == environmentID && snap.DeletedAt == nil {
			n++
		}
	}
	return n, nil
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
	return nil, errors.New("not used by environments")
}

// IAM resources.

func (s *store) RegisterResource(_ context.Context, r iam.Resource) error {
	s.resources[r.ID] = r
	return nil
}

func (s *store) ArchiveResource(_ context.Context, _, id string) error {
	s.archivedRes[id] = true
	return nil
}

func (s *store) RestoreResource(_ context.Context, _, id string) error {
	delete(s.archivedRes, id)
	return nil
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

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's
// resource ID.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

// world is a store with an active and an archived Workspace, and the
// EnvironmentService over it.
type world struct {
	*store
	authz *authorizer
	svc   delivery.EnvironmentService
}

const (
	wsActive      = "wsp_01JZ0000000000000000000001"
	wsArchived    = "wsp_01JZ0000000000000000000002"
	testClusterID = "cls_01JZ0000000000000000000001"
)

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	for _, ws := range []struct {
		id, slug string
		archived bool
	}{{wsActive, "platform", false}, {wsArchived, "legacy", true}} {
		id := ws.id
		w := must(workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: ws.slug, DisplayName: ws.slug}, &id))
		if ws.archived {
			require.NoError(t, w.Archive())
		}
		s.workspaces[id] = w
	}
	clusterID := testClusterID
	cluster := must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: org, Slug: "prod-1", DisplayName: "Prod", Runtime: delivery.RuntimeKubernetes,
	}, &clusterID))
	cluster.PullEvents()
	s.clusters[testClusterID] = cluster.Snapshot()

	authz := &authorizer{}
	svc, err := NewEnvironmentService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []EnvironmentServiceOption {
	return []EnvironmentServiceOption{
		WithEnvironmentRepo(s), WithTargetRepo(s), WithClusterRepo(s), WithLifecycleUsage(s), WithVariableUsage(s), WithWorkspaces(s),
		WithResourceRegistrar(s), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// env stores an Environment directly, bypassing the service.
func (w *world) env(t *testing.T, workspaceID, slug string, archived bool) *delivery.Environment {
	t.Helper()
	e := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Tier: delivery.TierStaging,
	}, nil))
	if archived {
		e.Archive()
	}
	e.PullEvents()
	require.NoError(t, w.CreateEnvironment(context.Background(), e))
	return e
}

// target stores a Target directly, bypassing the service.
func (w *world) target(t *testing.T, environmentID, slug string, archived bool) *delivery.Target {
	t.Helper()
	tg := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: environmentID, ClusterID: testClusterID,
		Slug: slug, DisplayName: slug,
		Runtime: delivery.RuntimeKubernetes, Locator: locatorIn(slug),
	}, nil))
	if archived {
		tg.Archive()
	}
	tg.PullEvents()
	require.NoError(t, w.CreateTarget(context.Background(), tg))
	return tg
}

func locator() map[string]string { return locatorIn("web") }

// locatorIn is the test Cluster's locator for a namespace.
func locatorIn(namespace string) map[string]string {
	return map[string]string{delivery.LocatorCluster: "prod-1", delivery.LocatorNamespace: namespace}
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.authz.asked = nil, nil, nil
}

func (s *store) CountActiveVariablesWithEnvironment(_ context.Context, _, environmentID string) (int, error) {
	return s.scopedVariables[environmentID], nil
}

func (s *store) CountActiveVariablesWithTarget(_ context.Context, _, targetID string) (int, error) {
	return s.scopedVariables[targetID], nil
}

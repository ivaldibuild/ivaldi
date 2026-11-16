package deployments

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const (
	org       = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	appID     = "app_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	envID     = "env_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	targetID  = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	clusterID = "cls_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	agentID   = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAG"
)

var (
	t0  = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ada = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: identity.ActorUser}
	ctx = context.Background()
)

type store struct {
	apps        map[string]*delivery.Application
	channels    map[string]*delivery.Channel
	lifecycles  map[string]*delivery.Lifecycle
	envs        map[string]*delivery.Environment
	targets     map[string]*delivery.Target
	clusters    map[string]*delivery.Cluster
	placements  map[string]*delivery.Placement
	builds      map[string]*delivery.Build
	artifacts   map[string]*delivery.Artifact
	releases    map[string]*delivery.Release
	deployments map[string]*delivery.Deployment
	events      []*delivery.DeploymentEvent
	audit       []audit.Event
	envelopes   []events.Envelope
	// lockedTargets lists the Targets read for update, in order.
	lockedTargets []string
	// revisions are the revisions of Releases' variables, by ID.
	revisions map[string]*delivery.ReleaseVariables
	// snapshotted lists the observed Releases whose variables were snapshotted.
	snapshotted []string
	// stores are the Secret Stores, by ID.
	stores map[string]delivery.SecretStoreRef
}

func newStore() *store {
	return &store{
		apps: map[string]*delivery.Application{}, channels: map[string]*delivery.Channel{},
		lifecycles: map[string]*delivery.Lifecycle{}, envs: map[string]*delivery.Environment{},
		targets: map[string]*delivery.Target{}, clusters: map[string]*delivery.Cluster{}, placements: map[string]*delivery.Placement{},
		builds: map[string]*delivery.Build{}, artifacts: map[string]*delivery.Artifact{},
		releases:    map[string]*delivery.Release{},
		deployments: map[string]*delivery.Deployment{},
	}
}

func placementKey(app, target string) string { return app + "/" + target }

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeApplications struct{ *store }

func (f fakeApplications) GetApplication(_ context.Context, organizationID, applicationID string) (*delivery.Application, error) {
	a, ok := f.apps[applicationID]
	if !ok || a.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return a, nil
}

func (f fakeApplications) GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*delivery.Application, error) {
	return f.GetApplication(ctx, organizationID, identifier)
}

func (f fakeApplications) GetPlacement(_ context.Context, organizationID, applicationID, targetID string) (*delivery.Placement, error) {
	p, ok := f.placements[placementKey(applicationID, targetID)]
	if !ok || p.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return p, nil
}

func (f fakeApplications) ListArtifacts(_ context.Context, organizationID, applicationID string, _ bool) ([]*delivery.Artifact, error) {
	var out []*delivery.Artifact
	for _, a := range f.artifacts {
		if a.OrganizationID() == organizationID && a.ApplicationID() == applicationID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f fakeApplications) GetChannel(_ context.Context, organizationID, channelID string) (*delivery.Channel, error) {
	c, ok := f.channels[channelID]
	if !ok || c.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return c, nil
}

func (f fakeApplications) ListPlacementsByTarget(_ context.Context, organizationID, targetID string) ([]*delivery.Placement, error) {
	var out []*delivery.Placement
	for _, p := range f.placements {
		if p.OrganizationID() == organizationID && p.TargetID() == targetID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f fakeApplications) UpdatePlacementCurrent(_ context.Context, p *delivery.Placement) error {
	f.placements[placementKey(p.ApplicationID(), p.TargetID())] = p
	return nil
}

func (f fakeApplications) GetDefaultChannel(_ context.Context, organizationID, applicationID string) (*delivery.Channel, error) {
	for _, c := range f.channels {
		if c.OrganizationID() == organizationID && c.ApplicationID() == applicationID && c.IsDefault() {
			return c, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (f fakeApplications) CreateArtifact(_ context.Context, a *delivery.Artifact) error {
	for _, existing := range f.artifacts {
		if existing.OrganizationID() == a.OrganizationID() && existing.ApplicationID() == a.ApplicationID() && existing.Name() == a.Name() {
			return delivery.ErrAlreadyExists
		}
	}
	f.artifacts[a.ID()] = a
	return nil
}

func (f fakeApplications) CreatePlacementIfAbsent(_ context.Context, p *delivery.Placement) (bool, error) {
	key := placementKey(p.ApplicationID(), p.TargetID())
	if _, ok := f.placements[key]; ok {
		return false, nil
	}
	f.placements[key] = p
	return true, nil
}

type fakeEnvironments struct{ *store }

func (f fakeEnvironments) GetEnvironment(_ context.Context, organizationID, environmentID string) (*delivery.Environment, error) {
	e, ok := f.envs[environmentID]
	if !ok || e.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return e, nil
}

type fakeTargets struct{ *store }

func (f fakeTargets) GetTarget(_ context.Context, organizationID, targetID string) (*delivery.Target, error) {
	t, ok := f.targets[targetID]
	if !ok || t.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return t, nil
}

func (f fakeTargets) GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*delivery.Target, error) {
	f.lockedTargets = append(f.lockedTargets, targetID)
	return f.GetTarget(ctx, organizationID, targetID)
}

func (f fakeTargets) ListTargets(_ context.Context, q delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	var out []*delivery.Target
	for _, t := range f.targets {
		if t.OrganizationID() != q.OrganizationID {
			continue
		}
		if q.EnvironmentID != "" && t.EnvironmentID() != q.EnvironmentID {
			continue
		}
		if q.ClusterID != "" && t.ClusterID() != q.ClusterID {
			continue
		}
		if !q.IncludeArchived && t.Archived() {
			continue
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b *delivery.Target) int { return strings.Compare(a.ID(), b.ID()) })
	return &pagination.Result[*delivery.Target]{Items: out, TotalCount: len(out)}, nil
}

func (f fakeTargets) UpdateTarget(_ context.Context, t *delivery.Target) error {
	f.targets[t.ID()] = t
	return nil
}

type fakeClusters struct{ *store }

func (f fakeClusters) GetCluster(_ context.Context, organizationID, id string) (*delivery.Cluster, error) {
	c, ok := f.clusters[id]
	if !ok || c.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return c, nil
}

func (f fakeClusters) GetClusterForShare(ctx context.Context, organizationID, id string) (*delivery.Cluster, error) {
	return f.GetCluster(ctx, organizationID, id)
}

type fakeLifecycles struct{ *store }

func (f fakeLifecycles) GetLifecycle(_ context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	l, ok := f.lifecycles[lifecycleID]
	if !ok || l.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return l, nil
}

type fakeReleases struct{ *store }

func (f fakeReleases) GetRelease(_ context.Context, organizationID, releaseID string) (*delivery.Release, error) {
	r, ok := f.releases[releaseID]
	if !ok || r.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return r, nil
}

func (f fakeReleases) GetReleaseByVersion(_ context.Context, organizationID, applicationID, version string) (*delivery.Release, error) {
	for _, r := range f.releases {
		if r.OrganizationID() == organizationID && r.ApplicationID() == applicationID && r.Version() == version {
			return r, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (f fakeReleases) CreateRelease(_ context.Context, r *delivery.Release) error {
	for _, existing := range f.releases {
		if existing.OrganizationID() == r.OrganizationID() && existing.ApplicationID() == r.ApplicationID() && existing.Version() == r.Version() {
			return delivery.ErrAlreadyExists
		}
	}
	f.releases[r.ID()] = r
	return nil
}

type fakeBuilds struct{ *store }

func (f fakeBuilds) GetBuilds(_ context.Context, organizationID string, buildIDs []string) ([]*delivery.Build, error) {
	var out []*delivery.Build
	for _, id := range buildIDs {
		if b, ok := f.builds[id]; ok && b.OrganizationID() == organizationID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f fakeBuilds) CreateBuild(_ context.Context, b *delivery.Build) (bool, error) {
	for _, existing := range f.builds {
		if existing.OrganizationID() == b.OrganizationID() && existing.ArtifactName() == b.ArtifactName() && existing.Digest() == b.Digest() {
			return false, nil
		}
	}
	f.builds[b.ID()] = b
	return true, nil
}

func (f fakeBuilds) GetBuildByDigestForUpdate(_ context.Context, organizationID, artifactName, digest string) (*delivery.Build, error) {
	for _, b := range f.builds {
		if b.OrganizationID() == organizationID && b.ArtifactName() == artifactName && b.Digest() == digest {
			return b, nil
		}
	}
	return nil, delivery.ErrNotFound
}

type fakeDeployments struct{ *store }

func (f fakeDeployments) CreateDeployment(_ context.Context, d *delivery.Deployment) error {
	f.deployments[d.ID()] = d
	return nil
}
func (f fakeDeployments) UpdateDeployment(_ context.Context, d *delivery.Deployment) error {
	f.deployments[d.ID()] = d
	return nil
}
func (f fakeDeployments) GetDeployment(_ context.Context, organizationID, deploymentID string) (*delivery.Deployment, error) {
	d, ok := f.deployments[deploymentID]
	if !ok || d.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return d, nil
}
func (f fakeDeployments) GetDeploymentForUpdate(ctx context.Context, organizationID, deploymentID string) (*delivery.Deployment, error) {
	return f.GetDeployment(ctx, organizationID, deploymentID)
}
func (f fakeDeployments) ListDeployments(_ context.Context, q delivery.ListDeploymentsQuery) (*pagination.Result[*delivery.Deployment], error) {
	var items []*delivery.Deployment
	for _, d := range f.deployments {
		if d.OrganizationID() != q.OrganizationID {
			continue
		}
		if q.ApplicationID != "" && d.ApplicationID() != q.ApplicationID {
			continue
		}
		if q.TargetID != "" && d.TargetID() != q.TargetID {
			continue
		}
		if q.ClusterID != "" {
			t, ok := f.targets[d.TargetID()]
			if !ok || t.ClusterID() != q.ClusterID || t.Archived() {
				continue
			}
		}
		if q.Status != "" && d.Status() != q.Status {
			continue
		}
		if len(q.Statuses) > 0 && !slices.Contains(q.Statuses, d.Status()) {
			continue
		}
		items = append(items, d)
	}
	return pagination.NewResult(items, len(items), false, false, nil, nil), nil
}
func (f fakeDeployments) GetInFlightDeployment(_ context.Context, organizationID, applicationID, targetID string) (*delivery.Deployment, error) {
	for _, d := range f.deployments {
		if d.OrganizationID() == organizationID && d.ApplicationID() == applicationID &&
			d.TargetID() == targetID && d.FinishedAt() == nil {
			return d, nil
		}
	}
	return nil, delivery.ErrNotFound
}
func (f fakeDeployments) GetLastSucceededDeployment(_ context.Context, organizationID, releaseID, targetID string) (*delivery.Deployment, error) {
	var last *delivery.Deployment
	for _, d := range f.deployments {
		if d.OrganizationID() != organizationID || d.ReleaseID() != releaseID || d.TargetID() != targetID || d.SucceededAt() == nil {
			continue
		}
		if last == nil || d.SucceededAt().After(*last.SucceededAt()) {
			last = d
		}
	}
	if last == nil {
		return nil, delivery.ErrNotFound
	}
	return last, nil
}
func (f fakeDeployments) ListSucceededEnvironmentIDs(_ context.Context, organizationID, releaseID string) ([]string, error) {
	var out []string
	for _, d := range f.deployments {
		if d.OrganizationID() == organizationID && d.ReleaseID() == releaseID && d.SucceededAt() != nil {
			out = append(out, d.EnvironmentID())
		}
	}
	return out, nil
}
func (f fakeDeployments) AppendDeploymentEvent(_ context.Context, e *delivery.DeploymentEvent) (bool, error) {
	if e.ReportID() != nil {
		for _, old := range f.events {
			if old.DeploymentID() == e.DeploymentID() && old.ReportID() != nil && *old.ReportID() == *e.ReportID() {
				return false, nil
			}
		}
	}
	f.events = append(f.events, e)
	return true, nil
}
func (f fakeDeployments) ListDeploymentEvents(_ context.Context, organizationID, deploymentID string) ([]*delivery.DeploymentEvent, error) {
	var out []*delivery.DeploymentEvent
	for _, e := range f.events {
		if e.OrganizationID() == organizationID && e.DeploymentID() == deploymentID {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeAuthorizer struct {
	asked    []authorization.Request
	outcome  authorization.Outcome
	onCreate authorization.Outcome
}

func (a *fakeAuthorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	outcome := a.outcome
	if outcome == "" {
		outcome = authorization.OutcomeAllow
	}
	if req.Permission == delivery.PermissionDeploymentCreate && a.onCreate != "" {
		outcome = a.onCreate
	}
	return authorization.Decision{ID: "dcn_01ARZ3NDEKTSV4RRFFQ69G5FAV", Outcome: outcome}, nil
}

type auditLog struct{ *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.audit = append(a.audit, e)
	return nil
}

type outboxLog struct{ *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.envelopes = append(o.envelopes, envs...)
	return nil
}

type world struct {
	*store
	svc     delivery.DeploymentService
	authz   *fakeAuthorizer
	history *fakeHistory
	changes *fakeChanges
	rel     *delivery.Release
	app     *delivery.Application
	env     *delivery.Environment
}

// fakeHistory is the commit history the version-control capability would
// provide.
type fakeHistory struct {
	commits []vcs.Commit
	err     error
}

func (f *fakeHistory) CommitsBetween(context.Context, string, string, string, string) ([]vcs.Commit, error) {
	return f.commits, f.err
}

// fakeChanges is what the provenance capability recorded.
type fakeChanges struct {
	changes []*provenance.Change
}

func (f *fakeChanges) ChangesForCommits(context.Context, string, string, []string) ([]*provenance.Change, error) {
	return f.changes, nil
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := newStore()
	app := mustApplication(org, appID)
	env := mustEnvironment(org, envID, true)
	cluster := mustCluster(org, clusterID, agentID)
	if err := cluster.RecordHeartbeat(agentID, map[string]any{"agent_version": "1.4.0"}); err != nil {
		panic(err)
	}
	target := mustTarget(org, envID, clusterID, targetID)
	if _, err := target.SetCapabilities(true, true, ""); err != nil {
		panic(err)
	}
	lc := mustLifecycle(org, envID)
	channel := mustChannel(org, appID, lc.ID())
	placement := mustPlacement(org, appID, targetID)
	s.apps[app.ID()] = app
	s.envs[env.ID()] = env
	s.targets[target.ID()] = target
	s.clusters[cluster.ID()] = cluster
	s.lifecycles[lc.ID()] = lc
	s.channels[channel.ID()] = channel
	s.placements[placementKey(appID, targetID)] = placement
	release, builds, artifact := mustRelease(org, appID, channel.ID())
	s.releases, s.builds = release, builds
	s.artifacts[artifact.ID()] = artifact
	authz := &fakeAuthorizer{}
	history := &fakeHistory{}
	recorded := &fakeChanges{}
	svc, err := NewService(
		WithDeploymentRepo(fakeDeployments{s}), WithReleaseRepo(fakeReleases{s}), WithBuildRepo(fakeBuilds{s}),
		WithApplications(fakeApplications{s}), WithEnvironments(fakeEnvironments{s}), WithTargets(fakeTargets{s}), WithClusters(fakeClusters{s}),
		WithLifecycles(fakeLifecycles{s}), WithVariables(s), WithVariableSnapshots(s), WithSecretStores(s), WithHeartbeatInterval(30*time.Second), WithCommitHistory(history), WithChanges(recorded), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz, history: history, changes: recorded, rel: s.releases[mustOneID(s.releases)], app: app, env: env}
}

func TestRequestDeployments(t *testing.T) {
	w := newWorld(t)
	out, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	d := out[0]
	assert.Equal(t, release.DeploymentApplying, d.Status())
	assert.NotNil(t, d.StartedAt())
	assert.Equal(t, w.app.ID(), d.ApplicationID())
	assert.Equal(t, delivery.EventDeploymentRequested, w.audit[0].Type)
	assert.Len(t, w.events, 1)
	assert.Len(t, w.envelopes, 1)
}

func TestRequestDeploymentsWaitsForApproval(t *testing.T) {
	w := newWorld(t)
	w.authz.onCreate = authorization.OutcomeRequiresApproval
	out, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.NoError(t, err)
	assert.Equal(t, release.DeploymentAwaitingApproval, out[0].Status())
	assert.Nil(t, out[0].StartedAt())
}

func TestRequestDeploymentsChecks(t *testing.T) {
	t.Run("no agent", func(t *testing.T) {
		w := newWorld(t)
		require.NoError(t, w.clusters[clusterID].UnassignAgent())
		_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
			OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
		})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})
	t.Run("the agent has not reported recently", func(t *testing.T) {
		w := newWorld(t)
		clock.Fix(t, t0.Add(time.Hour))
		_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
			OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
		})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})
	t.Run("the agent cannot execute", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.targets[targetID].SetCapabilities(true, false, "RBAC denies create on pods"); err != nil {
			t.Fatal(err)
		}
		_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
			OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
		})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})
	t.Run("execution disabled", func(t *testing.T) {
		w := newWorld(t)
		w.envs[envID] = mustEnvironment(org, envID, false)
		_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
			OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
		})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})
	t.Run("in flight", func(t *testing.T) {
		w := newWorld(t)
		d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
		w.deployments[d.ID()] = d
		_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
			OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
		})
		assert.ErrorIs(t, err, delivery.ErrDeploymentInFlight)
	})
}

func TestReportDeployment(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d
	agent := identity.Actor{PrincipalID: agentID, Kind: identity.ActorServiceAccount}
	status := release.DeploymentHealthy
	detail, err := w.svc.ReportDeployment(ctx, agent, delivery.ReportDeploymentInput{
		OrganizationID: org, DeploymentID: d.ID(), ReportID: "report-1",
		Kind: delivery.DeploymentEventHealth, Status: &status,
	})
	require.NoError(t, err)
	assert.Equal(t, release.DeploymentHealthy, detail.Deployment.Status)
	placement := w.placements[placementKey(appID, targetID)]
	assert.Equal(t, new(d.ID()), placement.CurrentDeploymentID())
	assert.Equal(t, new(w.rel.ID()), placement.CurrentReleaseID())

	_, err = w.svc.ReportDeployment(ctx, agent, delivery.ReportDeploymentInput{
		OrganizationID: org, DeploymentID: d.ID(), ReportID: "report-1",
		Kind: delivery.DeploymentEventHealth, Status: &status,
	})
	require.NoError(t, err, "a repeated report is idempotent")
}

func TestReportDeploymentRefusesAnotherAgent(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d
	status := release.DeploymentHealthy
	_, err := w.svc.ReportDeployment(ctx, ada, delivery.ReportDeploymentInput{
		OrganizationID: org, DeploymentID: d.ID(), ReportID: "report-1",
		Kind: delivery.DeploymentEventHealth, Status: &status,
	})
	assert.ErrorIs(t, err, delivery.ErrNotDeploymentAgent)
}

func TestCancelDeployment(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentAwaitingApproval)
	w.deployments[d.ID()] = d
	detail, err := w.svc.CancelDeployment(ctx, ada, org, d.ID())
	require.NoError(t, err)
	assert.Equal(t, release.DeploymentCancelled, detail.Deployment.Status)
	assert.Contains(t, w.auditTypes(), delivery.EventDeploymentCancelled)
}

func TestCancelDeploymentNeedingApprovalIsRefused(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d
	w.authz.onCreate = authorization.OutcomeRequiresApproval
	_, err := w.svc.CancelDeployment(ctx, ada, org, d.ID())
	var approval *authorization.ApprovalRequiredError
	assert.ErrorAs(t, err, &approval)
	assert.Equal(t, release.DeploymentApplying, w.deployments[d.ID()].Status())
	assert.NotContains(t, w.auditTypes(), delivery.EventDeploymentCancelled)
}

func TestListClusterAssignmentsChecksTheClusterAgent(t *testing.T) {
	w := newWorld(t)
	agent := identity.Actor{PrincipalID: agentID}
	_, err := w.svc.ListClusterAssignments(ctx, agent, org, clusterID)
	require.NoError(t, err)

	_, err = w.svc.ListClusterAssignments(ctx, ada, org, clusterID)
	assert.ErrorIs(t, err, delivery.ErrNotDeploymentAgent)
}

func TestListAssignmentsAsksOnlyForInFlightDeployments(t *testing.T) {
	w := newWorld(t)
	finished := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentHealthy)
	w.deployments[finished.ID()] = finished
	applying := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[applying.ID()] = applying
	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, applying.ID(), items[0].Deployment.ID)
}

func TestListAssignments(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d
	agent := identity.Actor{PrincipalID: agentID}
	items, err := w.svc.ListClusterAssignments(ctx, agent, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, d.ID(), items[0].Deployment.ID)
	require.NotNil(t, items[0].Apply.Helm)
	assert.Contains(t, items[0].Apply.Helm.ReleaseName, "app-")
	assert.Equal(t, "chart", items[0].Apply.Definition.ArtifactName)
	assert.Empty(t, items[0].Apply.Images)
}

func TestAssignmentShape(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d

	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	apply := items[0].Apply
	assert.Equal(t, delivery.DeployerHelm, apply.Deployer)
	assert.Equal(t, "default", apply.Namespace)
	assert.Equal(t, "chart", apply.Definition.ArtifactName)
	assert.Equal(t, delivery.ArtifactHelmChart, apply.Definition.Kind)
	assert.Equal(t, []string{}, apply.Overlays)
	assert.Empty(t, apply.Images)
	assert.Empty(t, apply.Variables)
	assert.Empty(t, apply.Secrets)
	require.NotNil(t, apply.Helm)
	assert.Contains(t, apply.Helm.ReleaseName, "app-")
	assert.Equal(t, delivery.DeploymentRollout, apply.Reason)
	assert.Nil(t, apply.Restores)
}

func TestAssignmentCarriesImagesAndOverlays(t *testing.T) {
	w := newWorld(t)
	image := mustArtifact(appID, "api", delivery.ArtifactContainerImage)
	imageDigest := "sha256:" + strings.Repeat("c", 64)
	imageBuild := must(delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: org, ArtifactKind: delivery.ArtifactContainerImage,
		ArtifactName: image.ArtifactName(), Digest: imageDigest, Provenance: delivery.BuildCIReported,
	}, nil))
	w.artifacts[image.ID()] = image
	w.builds[imageBuild.ID()] = imageBuild

	chart := w.releases[mustOneID(w.releases)]
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: org, ApplicationID: appID, ChannelID: chart.ChannelID(), Version: "2.0.0",
		Status: release.StatusPublished,
		Builds: []delivery.NewReleaseBuildProps{
			{ArtifactID: chart.Builds()[0].ArtifactID, BuildID: chart.Builds()[0].BuildID},
			{ArtifactID: image.ID(), BuildID: imageBuild.ID()},
		},
	}, nil))
	w.releases[rel.ID()] = rel
	d := mustDeployment(org, rel.ID(), appID, envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d

	placement := mustPlacement(org, appID, targetID)
	require.NoError(t, placement.Configure(delivery.ApplyConfig{
		DefinitionArtifact: "chart", Values: []string{"values.production.yaml"},
	}))
	w.placements[placementKey(appID, targetID)] = placement

	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	apply := items[0].Apply
	assert.Equal(t, []string{"values.production.yaml"}, apply.Overlays)
	require.Contains(t, apply.Images, "api")
	assert.Equal(t, "ghcr.io/acme/api", apply.Images["api"].Repository)
	assert.Equal(t, imageDigest, apply.Images["api"].Digest)
	assert.Equal(t, "ghcr.io/acme/api@"+imageDigest, apply.Images["api"].Image)
}

func TestReleaseChanges(t *testing.T) {
	w := newWorld(t)
	changes, err := w.svc.ReleaseChanges(ctx, ada, org, w.rel.ID(), targetID)
	require.NoError(t, err)
	require.Len(t, changes.Artifacts, 1)
	assert.Nil(t, changes.Artifacts[0].Previous)
	assert.Contains(t, changes.Artifacts[0].Unresolved, "the artifact is new on this Target")
	assert.Empty(t, changes.Changes)
	assert.NotEmpty(t, changes.Checks)
}

func TestReleaseChangesListsCommitsBetweenBuilds(t *testing.T) {
	w := newWorld(t)
	artifact := w.artifacts[mustOneID(w.artifacts)]
	previousBuild := ciBuild(artifact, "a", "aaaaaaaa")
	nextBuild := ciBuild(artifact, "b", "bbbbbbbb")
	w.builds[previousBuild.ID()] = previousBuild
	w.builds[nextBuild.ID()] = nextBuild

	previousRelease := releaseOf(w.rel, artifact, previousBuild, "0.9.0")
	nextRelease := releaseOf(w.rel, artifact, nextBuild, "1.0.0")
	w.releases[previousRelease.ID()] = previousRelease
	w.releases[nextRelease.ID()] = nextRelease

	deployment := mustDeployment(org, previousRelease.ID(), w.app.ID(), envID, targetID, release.DeploymentHealthy)
	placement := w.placements[placementKey(w.app.ID(), targetID)]
	require.NoError(t, placement.SetCurrent(deployment.ID(), previousRelease.ID()))
	w.placements[placementKey(w.app.ID(), targetID)] = placement

	committedAt := t0.Add(-time.Hour)
	w.history.commits = []vcs.Commit{{
		SHA: "bbbbbbbb", Message: "Add checkout\n\nDetails", AuthorName: "Ada",
		AuthorEmail: "ada@example.com", CommittedAt: committedAt,
	}}
	sessionID := "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	workItemID := "wki_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	recorded := must(provenance.NewChange(provenance.NewChangeProps{
		OrganizationID: org, SourceID: "src_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SessionID: &sessionID, WorkItemID: &workItemID, Title: "Add checkout",
		CommitSHAs: []string{"bbbbbbbb"},
	}, nil))
	w.changes.changes = []*provenance.Change{recorded}

	changes, err := w.svc.ReleaseChanges(ctx, ada, org, nextRelease.ID(), targetID)
	require.NoError(t, err)
	require.Len(t, changes.Artifacts, 1)
	item := changes.Artifacts[0]
	require.NotNil(t, item.Previous)
	assert.Equal(t, previousBuild.Digest(), item.Previous.Digest)
	assert.Equal(t, []string{"bbbbbbbb"}, item.CommitSHAs)
	assert.Empty(t, item.Unresolved)
	require.Len(t, changes.Changes, 1)
	assert.Equal(t, "bbbbbbbb", changes.Changes[0].CommitSHA)
	assert.Equal(t, "Add checkout", changes.Changes[0].Title)
	assert.Equal(t, committedAt, changes.Changes[0].CommittedAt)
	assert.Equal(t, recorded.ID(), *changes.Changes[0].ChangeID)
	assert.Equal(t, sessionID, *changes.Changes[0].SessionID)
	assert.Equal(t, workItemID, *changes.Changes[0].WorkItemID)

	// A Build with no commit makes the range unreadable, but not the read.
	withoutCommit := ciBuild(artifact, "c", "")
	w.builds[withoutCommit.ID()] = withoutCommit
	nextWithoutCommit := releaseOf(w.rel, artifact, withoutCommit, "1.0.1")
	w.releases[nextWithoutCommit.ID()] = nextWithoutCommit
	changes, err = w.svc.ReleaseChanges(ctx, ada, org, nextWithoutCommit.ID(), targetID)
	require.NoError(t, err)
	require.Len(t, changes.Artifacts, 1)
	assert.Contains(t, changes.Artifacts[0].Unresolved, "the Build has no commit")
	assert.Empty(t, changes.Changes)
}

// ciBuild returns a CI-reported Build from source src at commit.
func ciBuild(artifact *delivery.Artifact, digest, commit string) *delivery.Build {
	source := "src_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	return must(delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: org, ArtifactKind: artifact.ArtifactKind(), ArtifactName: artifact.ArtifactName(),
		Digest:     "sha256:" + strings.Repeat(digest, 64),
		Provenance: delivery.BuildCIReported,
		Details:    delivery.BuildDetails{SourceID: &source, CommitSHA: &commit},
	}, nil))
}

// releaseOf returns a published Release like template with one Build.
func releaseOf(template *delivery.Release, artifact *delivery.Artifact, build *delivery.Build, version string) *delivery.Release {
	return must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: org, ApplicationID: template.ApplicationID(), ChannelID: template.ChannelID(),
		Version: version, Status: release.StatusPublished,
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: artifact.ID(), BuildID: build.ID()}},
	}, nil))
}

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

func mustArtifact(appID, name string, kind delivery.ArtifactKind) *delivery.Artifact {
	return must(delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: org, ApplicationID: appID, Name: name, ArtifactKind: kind,
		ArtifactName: "ghcr.io/acme/" + name,
	}, nil))
}

func mustApplication(org, id string) *delivery.Application {
	return must(delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: org, WorkspaceID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID: "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV", OwnerTeamID: "tem_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		DisplayName: "API",
	}, &id))
}

func mustEnvironment(org, id string, execution bool) *delivery.Environment {
	return must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: org, WorkspaceID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		DisplayName: "Production", Tier: delivery.TierProduction, ExecutionEnabled: execution,
	}, &id))
}

func mustCluster(org, id, agent string) *delivery.Cluster {
	c := must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: org, Slug: "cluster", DisplayName: "Prod cluster", Runtime: delivery.RuntimeKubernetes,
	}, &id))
	if err := c.AssignAgent(agent); err != nil {
		panic(err)
	}
	return c
}

func mustTarget(org, envID, clusterID, id string) *delivery.Target {
	return must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: envID, ClusterID: clusterID, DisplayName: "Prod cluster",
		Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "cluster", delivery.LocatorNamespace: "default"},
	}, &id))
}

func mustLifecycle(org, envID string) *delivery.Lifecycle {
	return must(delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: org, WorkspaceID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", DisplayName: "Mainline",
		Phases: []delivery.NewLifecyclePhaseProps{{Name: "Production", EnvironmentIDs: []string{envID}}},
	}, nil))
}

func mustChannel(org, appID, lifecycleID string) *delivery.Channel {
	return must(delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: org, ApplicationID: appID, LifecycleID: lifecycleID, DisplayName: "Main",
		IsDefault: true,
	}, nil))
}

func mustPlacement(org, appID, targetID string) *delivery.Placement {
	return must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: org, ApplicationID: appID, TargetID: targetID,
		ApplyConfig: &delivery.ApplyConfig{DefinitionArtifact: "chart"},
	}))
}

func mustRelease(org, appID, channelID string) (map[string]*delivery.Release, map[string]*delivery.Build, *delivery.Artifact) {
	artifact := mustArtifact(appID, "chart", delivery.ArtifactHelmChart)
	build := must(delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: org, ArtifactKind: delivery.ArtifactHelmChart, ArtifactName: artifact.ArtifactName(),
		Digest:     "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Provenance: delivery.BuildCIReported,
	}, nil))
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: org, ApplicationID: appID, ChannelID: channelID, Version: "1.0.0",
		Status: release.StatusPublished,
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: artifact.ID(), BuildID: build.ID()}},
	}, nil))
	return map[string]*delivery.Release{rel.ID(): rel}, map[string]*delivery.Build{build.ID(): build}, artifact
}

func mustDeployment(org, releaseID, appID, envID, targetID string, status release.DeploymentStatus) *delivery.Deployment {
	return must(delivery.NewDeployment(delivery.NewDeploymentProps{
		OrganizationID: org, ApplicationID: appID, ReleaseID: releaseID, EnvironmentID: envID,
		TargetID: targetID, Status: status,
	}, nil))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func requireNoErr(err error) {
	if err != nil {
		panic(err)
	}
}

func mustOneID[V any](m map[string]V) string {
	for id := range m {
		return id
	}
	panic("empty map")
}

const secondTargetID = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAW"

func TestListClusterAssignmentsAcrossTargets(t *testing.T) {
	w := newWorld(t)
	second := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: envID, ClusterID: clusterID,
		DisplayName: "Second", Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "cluster", delivery.LocatorNamespace: "second"},
	}, new(secondTargetID)))
	w.targets[second.ID()] = second
	w.placements[placementKey(appID, second.ID())] = mustPlacement(org, appID, second.ID())
	first := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[first.ID()] = first
	other := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, second.ID(), release.DeploymentApplying)
	w.deployments[other.ID()] = other

	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	assert.Len(t, items, 2)
}

func TestListClusterAssignmentsFailsOnlyWhatCannotBeApplied(t *testing.T) {
	w := newWorld(t)
	second := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: envID, ClusterID: clusterID,
		DisplayName: "Second", Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "cluster", delivery.LocatorNamespace: "second"},
	}, new(secondTargetID)))
	w.targets[second.ID()] = second
	// The placement lost its apply config after the Deployment was requested.
	w.placements[placementKey(appID, second.ID())] = must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: org, ApplicationID: appID, TargetID: second.ID(), Source: delivery.PlacementObserved,
	}))
	healthy := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[healthy.ID()] = healthy
	broken := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, second.ID(), release.DeploymentApplying)
	w.deployments[broken.ID()] = broken

	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)

	require.NoError(t, err, "one Deployment that cannot be applied does not cut the agent off")
	require.Len(t, items, 1)
	assert.Equal(t, healthy.ID(), items[0].Deployment.ID)
	assert.Equal(t, release.DeploymentFailed, w.deployments[broken.ID()].Status(), "the Deployment that cannot be applied fails")
	require.NotEmpty(t, w.events)
	last := w.events[len(w.events)-1]
	assert.Equal(t, broken.ID(), last.DeploymentID())
	assert.Contains(t, string(last.Snapshot().Detail), "apply config", "its timeline says why")
	assert.Equal(t, delivery.EventDeploymentFailed, w.audit[len(w.audit)-1].Type)
	assert.True(t, w.audit[len(w.audit)-1].Actor.IsSystem())

	items, err = w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	assert.Len(t, items, 1, "a failed Deployment is no longer assigned")
}

func TestObserve(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	decision := authorization.WithDecision(ctx, "", authorization.Decision{Outcome: authorization.OutcomeAllow})
	agent := identity.Actor{PrincipalID: agentID, Kind: identity.ActorServiceAccount}
	observe := func(t *testing.T, w *world, reportID, digest string) *delivery.DeploymentDetail {
		t.Helper()
		detail, err := w.svc.Observe(decision, agent, delivery.ObserveInput{
			OrganizationID: org, ClusterID: clusterID, TargetID: targetID, ApplicationID: appID,
			Artifacts:   []delivery.ObservedArtifact{{ArtifactName: "ghcr.io/acme/api", Digest: digest}},
			Workloads:   []delivery.ObservedWorkload{{Kind: "Deployment", Namespace: "default", Name: "api"}},
			ExternalRef: json.RawMessage(`{"type":"helm","name":"api","revision":"7"}`),
			ReportID:    reportID,
		})
		require.NoError(t, err)
		return detail
	}

	t.Run("records an observed deployment, artifact, and build", func(t *testing.T) {
		w := newWorld(t)
		detail := observe(t, w, "obs-1", digest)

		assert.Equal(t, delivery.DeploymentObserved, detail.Deployment.Source)
		assert.Equal(t, release.DeploymentHealthy, detail.Deployment.Status)
		assert.Equal(t, appID, detail.Deployment.ApplicationID)
		require.Len(t, detail.Events, 1, "the initial status is on the timeline")

		placement := w.placements[placementKey(appID, targetID)]
		require.NotNil(t, placement.CurrentDeploymentID())
		assert.Equal(t, detail.Deployment.ID, *placement.CurrentDeploymentID())
		require.NotNil(t, placement.CurrentReleaseID())
		assert.Equal(t, detail.Deployment.ReleaseID, *placement.CurrentReleaseID())

		var observed *delivery.Artifact
		for _, a := range w.artifacts {
			if a.ArtifactName() == "ghcr.io/acme/api" {
				observed = a
			}
		}
		require.NotNil(t, observed, "the unmatched image becomes an observed artifact")
		assert.Equal(t, delivery.ArtifactObserved, observed.Source())
		assert.Equal(t, "api", observed.Name())

		var build *delivery.Build
		for _, b := range w.builds {
			if b.ArtifactName() == "ghcr.io/acme/api" {
				build = b
			}
		}
		require.NotNil(t, build)
		assert.Equal(t, delivery.BuildObserved, build.Provenance())
		assert.Equal(t, digest, build.Digest())
		assert.Contains(t, w.auditTypes(), delivery.EventDeploymentObserved)
	})

	t.Run("the same images change nothing", func(t *testing.T) {
		w := newWorld(t)
		first := observe(t, w, "obs-1", digest)
		deployments, releases, artifacts := len(w.deployments), len(w.releases), len(w.artifacts)

		second := observe(t, w, "obs-2", digest)

		assert.Equal(t, first.Deployment.ID, second.Deployment.ID, "the current deployment is returned")
		assert.Len(t, w.deployments, deployments)
		assert.Len(t, w.releases, releases)
		assert.Len(t, w.artifacts, artifacts)
	})

	t.Run("a chart in the current release is not an image", func(t *testing.T) {
		w := newWorld(t)
		image := mustArtifact(appID, "api", delivery.ArtifactContainerImage)
		imageBuild := must(delivery.NewBuild(delivery.NewBuildProps{
			OrganizationID: org, ArtifactKind: delivery.ArtifactContainerImage,
			ArtifactName: image.ArtifactName(), Digest: digest, Provenance: delivery.BuildCIReported,
		}, nil))
		w.artifacts[image.ID()] = image
		w.builds[imageBuild.ID()] = imageBuild

		chart := w.releases[mustOneID(w.releases)]
		rel := must(delivery.NewRelease(delivery.NewReleaseProps{
			OrganizationID: org, ApplicationID: appID, ChannelID: chart.ChannelID(), Version: "2.0.0",
			Status: release.StatusPublished,
			Builds: []delivery.NewReleaseBuildProps{
				{ArtifactID: chart.Builds()[0].ArtifactID, BuildID: chart.Builds()[0].BuildID},
				{ArtifactID: image.ID(), BuildID: imageBuild.ID()},
			},
		}, nil))
		w.releases[rel.ID()] = rel
		current := mustDeployment(org, rel.ID(), appID, envID, targetID, release.DeploymentHealthy)
		w.deployments[current.ID()] = current
		placement := w.placements[placementKey(appID, targetID)]
		_ = placement.SetCurrent(current.ID(), rel.ID())
		w.placements[placementKey(appID, targetID)] = placement

		detail := observe(t, w, "obs-1", digest)
		assert.Equal(t, current.ID(), detail.Deployment.ID, "the chart does not make the image set differ")
	})

	t.Run("a changed digest supersedes the previous deployment", func(t *testing.T) {
		w := newWorld(t)
		first := observe(t, w, "obs-1", digest)
		next := "sha256:" + strings.Repeat("d", 64)
		second := observe(t, w, "obs-2", next)

		assert.NotEqual(t, first.Deployment.ID, second.Deployment.ID)
		assert.Equal(t, release.DeploymentSuperseded, w.deployments[first.Deployment.ID].Status())
		placement := w.placements[placementKey(appID, targetID)]
		assert.Equal(t, second.Deployment.ID, *placement.CurrentDeploymentID())
	})

	t.Run("a rollout in progress owns the placement", func(t *testing.T) {
		w := newWorld(t)
		rollout := mustDeployment(org, mustOneID(w.releases), appID, envID, targetID, release.DeploymentProgressing)
		w.deployments[rollout.ID()] = rollout
		deployments, releases, artifacts := len(w.deployments), len(w.releases), len(w.artifacts)

		detail := observe(t, w, "obs-1", digest)

		assert.Equal(t, rollout.ID(), detail.Deployment.ID, "the rollout's Deployment is returned")
		assert.Len(t, w.deployments, deployments, "no observed Deployment is recorded mid-rollout")
		assert.Len(t, w.releases, releases)
		assert.Len(t, w.artifacts, artifacts)
	})

	t.Run("a deployment awaiting approval does not hold observations back", func(t *testing.T) {
		w := newWorld(t)
		waiting := mustDeployment(org, mustOneID(w.releases), appID, envID, targetID, release.DeploymentAwaitingApproval)
		w.deployments[waiting.ID()] = waiting

		detail := observe(t, w, "obs-1", digest)

		assert.NotEqual(t, waiting.ID(), detail.Deployment.ID)
		assert.Equal(t, delivery.DeploymentObserved, detail.Deployment.Source)
	})

	t.Run("the workloads are on the first status event", func(t *testing.T) {
		w := newWorld(t)
		detail := observe(t, w, "obs-1", digest)

		require.Len(t, detail.Events, 1)
		assert.JSONEq(t, `{"workloads":[{"kind":"Deployment","namespace":"default","name":"api"}]}`, string(detail.Events[0].Detail))
	})

	t.Run("an observation from the future is capped at now", func(t *testing.T) {
		w := newWorld(t)
		future := clock.Now().Add(time.Hour)
		detail, err := w.svc.Observe(decision, agent, delivery.ObserveInput{
			OrganizationID: org, ClusterID: clusterID, TargetID: targetID, ApplicationID: appID,
			Artifacts:  []delivery.ObservedArtifact{{ArtifactName: "ghcr.io/acme/api", Digest: digest}},
			ReportID:   "obs-1",
			ObservedAt: &future,
		})
		require.NoError(t, err)
		require.NotNil(t, detail.Deployment.StartedAt)
		assert.Equal(t, clock.Now(), *detail.Deployment.StartedAt, "a skewed agent clock cannot date a Deployment in the future")
	})
}

func (s *store) GetReleaseVariables(_ context.Context, _, revisionID string) (*delivery.ReleaseVariables, error) {
	if rv, ok := s.revisions[revisionID]; ok {
		return rv, nil
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetCurrentReleaseVariables(_ context.Context, _, releaseID string) (*delivery.ReleaseVariables, error) {
	for _, rv := range s.revisions {
		if rv.ReleaseID() == releaseID && rv.Status() == delivery.ReleaseVariablesCurrent {
			return rv, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) Take(_ context.Context, rel *delivery.Release, _ string, _ *string) ([]*delivery.ReleaseVariables, error) {
	s.snapshotted = append(s.snapshotted, rel.ID())
	return nil, nil
}

// withVariables gives the world's Release a current revision holding the
// variables, and returns it.
func (w *world) withVariables(variables ...delivery.SnapshotVariable) *delivery.ReleaseVariables {
	rv, err := delivery.NewReleaseVariables(delivery.NewReleaseVariablesProps{
		OrganizationID: org, ApplicationID: w.app.ID(), ReleaseID: w.rel.ID(), Revision: 1, Variables: variables,
	}, nil)
	if err != nil {
		panic(err)
	}
	if w.revisions == nil {
		w.revisions = map[string]*delivery.ReleaseVariables{}
	}
	w.revisions[rv.ID()] = rv
	return rv
}

func textVariable(name, path string, values ...delivery.SnapshotValue) delivery.SnapshotVariable {
	v := delivery.SnapshotVariable{Name: name, Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText, Values: values}
	if path != "" {
		v.ValuesPath = &path
	}
	return v
}

func TestADeploymentUsesItsReleasesVariables(t *testing.T) {
	w := newWorld(t)
	revision := w.withVariables(
		textVariable("Log.Level", "log.level",
			delivery.SnapshotValue{Value: "info"},
			delivery.SnapshotValue{Value: "warn", Scope: delivery.VariableScope{EnvironmentIDs: []string{envID}}}),
		textVariable("Banner", "", delivery.SnapshotValue{Value: "#{Log.Level} in #{Ivaldi.Environment.Slug}"}),
		textVariable("Elsewhere", "elsewhere", delivery.SnapshotValue{Value: "x", Scope: delivery.VariableScope{TargetIDs: []string{"tgt_01BX5ZZKBKACTAV9WEVGEMMVZZ"}}}),
	)
	out, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.NoError(t, err)
	require.NotNil(t, out[0].VariableSnapshotID(), "the deployment records the revision it used")
	assert.Equal(t, revision.ID(), *out[0].VariableSnapshotID())

	// A later revision does not change what a requested Deployment applies.
	revision.Supersede()
	w.withVariables(textVariable("Log.Level", "log.level", delivery.SnapshotValue{Value: "debug"}))

	items, err := w.svc.ListClusterAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, []delivery.AssignmentVariable{
		{Name: "Banner", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText, Value: "#{Log.Level} in #{Ivaldi.Environment.Slug}"},
		{Name: "Log.Level", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindText, ValuesPath: "log.level", Value: "warn"},
	}, items[0].Apply.Variables, "each variable's winning value for the target; none for a variable with no match")
}

func TestRequestDeploymentsRefusesABindingWithNoValue(t *testing.T) {
	w := newWorld(t)
	w.withVariables(textVariable("Database.Url", "database.url", delivery.SnapshotValue{Value: "postgres://#{Database.Host}/app"}))

	_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.ErrorIs(t, err, delivery.ErrInvalid)
	assert.Contains(t, err.Error(), "#{Database.Host}")
	assert.Empty(t, w.deployments)

	changes, err := w.svc.ReleaseChanges(ctx, ada, org, w.rel.ID(), targetID)
	require.NoError(t, err)
	failed := false
	for _, c := range changes.Checks {
		if c.Name == "variables_defined" {
			failed = !c.OK
		}
	}
	assert.True(t, failed, "the deploy sheet says so before it is requested")
}

func (s *store) LookupSecretStoresForShare(ctx context.Context, organizationID string, ids []string) (map[string]delivery.SecretStoreRef, error) {
	return s.LookupSecretStores(ctx, organizationID, ids)
}

func (s *store) LookupSecretStores(_ context.Context, _ string, ids []string) (map[string]delivery.SecretStoreRef, error) {
	out := map[string]delivery.SecretStoreRef{}
	for _, id := range ids {
		if ref, ok := s.stores[id]; ok {
			out[id] = ref
		}
	}
	return out, nil
}

// The agent is sent where each secret is kept, reports the version and a
// fingerprint of what it read, and the Deployment records them: never a value.
func TestADeploymentRecordsTheSecretsItsAgentResolved(t *testing.T) {
	const storeID = "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	w := newWorld(t)
	require.NoError(t, w.clusters[clusterID].RecordHeartbeat(agentID, map[string]any{"secret_store_kinds": []string{"kubernetes"}}))
	settings := delivery.StoreSettings{ID: storeID, Kind: delivery.StoreKubernetes, Kubernetes: &delivery.KubernetesStore{}}
	w.stores = map[string]delivery.SecretStoreRef{storeID: {ID: storeID, DisplayName: "In-cluster", Settings: settings}}
	w.withVariables(delivery.SnapshotVariable{
		Name: "Database.Password", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindSecret,
		DeliverAs: new(delivery.VariableDeliverAsSecret),
		Values:    []delivery.SnapshotValue{{Secret: &delivery.SecretReference{StoreID: storeID, Path: "db", Field: "password"}}},
	})

	out, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.NoError(t, err)
	agent := identity.Actor{PrincipalID: agentID}
	items, err := w.svc.ListClusterAssignments(ctx, agent, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, []delivery.AssignmentSecret{{
		Name: "Database.Password", Source: delivery.VariableSourceApplication, DeliverAs: delivery.VariableDeliverAsSecret,
		Store: settings, Ref: delivery.SecretRef{Path: "db", Field: "password"},
	}}, items[0].Apply.Secrets)

	report := func(reportID, detail string) error {
		_, err := w.svc.ReportDeployment(ctx, agent, delivery.ReportDeploymentInput{
			OrganizationID: org, DeploymentID: out[0].ID(), Kind: delivery.DeploymentEventHealth,
			Status: new(release.DeploymentProgressing), Detail: json.RawMessage(detail), ReportID: reportID,
		})
		return err
	}
	require.ErrorIs(t, report("refused", `{"secrets":[{"name":"Other","fingerprint":"hmac-sha256:v1:aa"}]}`), delivery.ErrInvalid,
		"a secret the deployment was not sent")
	// An overlay bound the secret, so its value is in the chart's values too.
	require.NoError(t, report("applied", `{"secrets":[{"name":"Database.Password","version":"4812","fingerprint":"hmac-sha256:v1:ab12","delivered_as":"secret"}],"secrets_in_values":["Database.Password"]}`))

	assert.Equal(t, []delivery.SecretVersion{{
		Name: "Database.Password", StoreID: storeID, Path: "db", Field: "password",
		Version: "4812", Fingerprint: "hmac-sha256:v1:ab12", DeliveredAs: delivery.VariableDeliverAsValue,
	}}, w.deployments[out[0].ID()].SecretVersions())

	// The agent reads no secret to watch an applied Deployment, so a store
	// archived since does not fail it.
	w.stores = nil
	items, err = w.svc.ListClusterAssignments(ctx, agent, org, clusterID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, release.DeploymentProgressing, w.deployments[out[0].ID()].Status())
}

func TestRequestDeploymentsRefusesASecretItsTargetCannotUse(t *testing.T) {
	const storeID = "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	w := newWorld(t)
	w.stores = map[string]delivery.SecretStoreRef{storeID: {
		ID: storeID, DisplayName: "Vault", EnvironmentIDs: []string{"env_01BX5ZZKBKACTAV9WEVGEMMVZZ"},
		Settings: delivery.StoreSettings{ID: storeID, Kind: delivery.StoreVault},
	}}
	w.withVariables(delivery.SnapshotVariable{
		Name: "Api.Key", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindSecret,
		Values: []delivery.SnapshotValue{{Secret: &delivery.SecretReference{StoreID: storeID, Path: "api"}}},
	})
	_, err := w.svc.RequestDeployments(ctx, ada, delivery.RequestDeploymentsInput{
		OrganizationID: org, ReleaseID: w.rel.ID(), TargetIDs: []string{targetID},
	})
	require.ErrorIs(t, err, delivery.ErrInvalid)
	assert.Contains(t, err.Error(), "not allowed for this environment")
}

// A rollback replays the Release's last healthy Deployment on the Target:
// its revision of variables, not the Release's current one, and the secret
// versions it resolved, unless it is asked for the latest.
func TestRollbackReplaysTheDeploymentItRestores(t *testing.T) {
	const storeID = "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	w := newWorld(t)
	agent := identity.Actor{PrincipalID: agentID}
	require.NoError(t, w.clusters[clusterID].RecordHeartbeat(agentID, map[string]any{"secret_store_kinds": []string{"kubernetes"}}))
	w.stores = map[string]delivery.SecretStoreRef{storeID: {
		ID: storeID, DisplayName: "In-cluster",
		Settings: delivery.StoreSettings{ID: storeID, Kind: delivery.StoreKubernetes, Kubernetes: &delivery.KubernetesStore{}},
	}}
	password := delivery.SnapshotVariable{
		Name: "Database.Password", Source: delivery.VariableSourceApplication, Kind: delivery.VariableKindSecret,
		DeliverAs: new(delivery.VariableDeliverAsSecret),
		Values:    []delivery.SnapshotValue{{Secret: &delivery.SecretReference{StoreID: storeID, Path: "db", Version: "40"}}},
	}
	released := w.withVariables(password, textVariable("Log.Level", "log.level", delivery.SnapshotValue{Value: "info"}))

	// deploy requests a Deployment and has the agent report it healthy.
	deploy := func(rel *delivery.Release, in delivery.RequestDeploymentsInput, secrets string) *delivery.Deployment {
		t.Helper()
		in.OrganizationID, in.ReleaseID, in.TargetIDs = org, rel.ID(), []string{targetID}
		out, err := w.svc.RequestDeployments(ctx, ada, in)
		require.NoError(t, err)
		for _, status := range []release.DeploymentStatus{release.DeploymentProgressing, release.DeploymentHealthy} {
			detail := json.RawMessage(nil)
			if status == release.DeploymentProgressing && secrets != "" {
				detail = json.RawMessage(secrets)
			}
			_, err := w.svc.ReportDeployment(ctx, agent, delivery.ReportDeploymentInput{
				OrganizationID: org, DeploymentID: out[0].ID(), Kind: delivery.DeploymentEventHealth,
				Status: &status, Detail: detail, ReportID: out[0].ID() + ":" + string(status),
			})
			require.NoError(t, err)
		}
		clock.Fix(t, clock.Now().Add(time.Second))
		return w.deployments[out[0].ID()]
	}
	reported := `{"secrets":[{"name":"Database.Password","version":"41","fingerprint":"hmac-sha256:v1:first"}]}`

	first := deploy(w.rel, delivery.RequestDeploymentsInput{}, reported)
	assert.Equal(t, delivery.DeploymentRollout, first.Reason())

	again := deploy(w.rel, delivery.RequestDeploymentsInput{}, `{"secrets":[{"name":"Database.Password","version":"42","fingerprint":"hmac-sha256:v1:rotated"}]}`)
	assert.Equal(t, delivery.DeploymentRedeploy, again.Reason(), "the release is the one running there")
	assert.Nil(t, again.RestoresDeploymentID())

	// Another Release takes over the Target, and the first one's variables
	// are refreshed meanwhile.
	builds := make([]delivery.NewReleaseBuildProps, 0, len(w.rel.Builds()))
	for _, b := range w.rel.Builds() {
		builds = append(builds, delivery.NewReleaseBuildProps{ArtifactID: b.ArtifactID, BuildID: b.BuildID})
	}
	now := clock.Now()
	next := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: org, ApplicationID: w.app.ID(), ChannelID: w.rel.ChannelID(), Version: "2.0.0",
		Status: release.StatusPublished, PublishedAt: &now, Builds: builds,
	}, nil))
	w.releases[next.ID()] = next
	assert.Equal(t, delivery.DeploymentRollout, deploy(next, delivery.RequestDeploymentsInput{}, "").Reason())
	released.Supersede()
	w.withVariables(password, textVariable("Log.Level", "log.level", delivery.SnapshotValue{Value: "debug"}))

	changes, err := w.svc.ReleaseChanges(ctx, ada, org, w.rel.ID(), targetID)
	require.NoError(t, err)
	assert.Equal(t, delivery.DeploymentRollback, changes.Reason, "the deploy sheet says so before it is requested")
	assert.Equal(t, again.ID(), *changes.RestoresDeploymentID)

	assignment := func(d *delivery.Deployment) delivery.ApplyAssignment {
		t.Helper()
		items, err := w.svc.ListClusterAssignments(ctx, agent, org, clusterID)
		require.NoError(t, err)
		for _, item := range items {
			if item.Deployment.ID == d.ID() {
				return item.Apply
			}
		}
		t.Fatalf("deployment %s has no assignment", d.ID())
		return delivery.ApplyAssignment{}
	}
	request := func(in delivery.RequestDeploymentsInput) *delivery.Deployment {
		t.Helper()
		in.OrganizationID, in.ReleaseID, in.TargetIDs = org, w.rel.ID(), []string{targetID}
		out, err := w.svc.RequestDeployments(ctx, ada, in)
		require.NoError(t, err)
		return out[0]
	}

	rollback := request(delivery.RequestDeploymentsInput{})
	assert.Equal(t, delivery.DeploymentRollback, rollback.Reason())
	assert.Equal(t, again.ID(), *rollback.RestoresDeploymentID(), "the last healthy deployment of the release there")
	assert.Equal(t, released.ID(), *rollback.VariableSnapshotID(), "the revision it ran with, not the refreshed one")
	apply := assignment(rollback)
	assert.Equal(t, delivery.DeploymentRollback, apply.Reason)
	assert.Equal(t, again.ID(), *apply.Restores)
	assert.Equal(t, "info", apply.Variables[0].Value)
	require.Len(t, apply.Secrets, 1)
	assert.Equal(t, "42", apply.Secrets[0].Ref.Version, "the version it read then")
	assert.Equal(t, "hmac-sha256:v1:rotated", apply.Secrets[0].Fingerprint, "which the agent checks the value against")

	// With the latest secrets it keeps the restored variables and pins
	// nothing, not even the version the variable itself names.
	delete(w.deployments, rollback.ID())
	latest := request(delivery.RequestDeploymentsInput{SecretsLatest: true})
	assert.True(t, latest.SecretsLatest())
	assert.Equal(t, released.ID(), *latest.VariableSnapshotID())
	apply = assignment(latest)
	assert.Empty(t, apply.Secrets[0].Ref.Version)
	assert.Empty(t, apply.Secrets[0].Fingerprint)
}

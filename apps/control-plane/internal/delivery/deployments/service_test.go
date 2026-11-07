package deployments

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

const (
	org      = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	appID    = "app_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	envID    = "env_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	targetID = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	agentID  = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAG"
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
}

func newStore() *store {
	return &store{
		apps: map[string]*delivery.Application{}, channels: map[string]*delivery.Channel{},
		lifecycles: map[string]*delivery.Lifecycle{}, envs: map[string]*delivery.Environment{},
		targets: map[string]*delivery.Target{}, placements: map[string]*delivery.Placement{},
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

func (f fakeApplications) UpdatePlacementCurrent(_ context.Context, p *delivery.Placement) error {
	f.placements[placementKey(p.ApplicationID(), p.TargetID())] = p
	return nil
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

func (f fakeTargets) GetTargetByAgentPrincipalID(_ context.Context, organizationID, principalID string) (*delivery.Target, error) {
	for _, t := range f.targets {
		if t.OrganizationID() == organizationID && t.AgentPrincipalID() != nil && *t.AgentPrincipalID() == principalID && !t.Archived() {
			return t, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (f fakeTargets) UpdateTarget(_ context.Context, t *delivery.Target) error {
	f.targets[t.ID()] = t
	return nil
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
	svc   delivery.DeploymentService
	authz *fakeAuthorizer
	rel   *delivery.Release
	app   *delivery.Application
	env   *delivery.Environment
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := newStore()
	app := mustApplication(org, appID)
	env := mustEnvironment(org, envID, true)
	target := mustTarget(org, envID, targetID, agentID)
	lc := mustLifecycle(org, envID)
	channel := mustChannel(org, appID, lc.ID())
	placement := mustPlacement(org, appID, targetID)
	s.apps[app.ID()] = app
	s.envs[env.ID()] = env
	s.targets[target.ID()] = target
	s.lifecycles[lc.ID()] = lc
	s.channels[channel.ID()] = channel
	s.placements[placementKey(appID, targetID)] = placement
	release, builds, artifact := mustRelease(org, appID, channel.ID())
	s.releases, s.builds = release, builds
	s.artifacts[artifact.ID()] = artifact
	authz := &fakeAuthorizer{}
	svc, err := NewService(
		WithDeploymentRepo(fakeDeployments{s}), WithReleaseRepo(fakeReleases{s}), WithBuildRepo(fakeBuilds{s}),
		WithApplications(fakeApplications{s}), WithEnvironments(fakeEnvironments{s}), WithTargets(fakeTargets{s}),
		WithLifecycles(fakeLifecycles{s}), WithAuthorizer(authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz, rel: s.releases[mustOneID(s.releases)], app: app, env: env}
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
		target := w.targets[targetID]
		require.NoError(t, target.UnassignAgent())
		w.targets[targetID] = target
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
	assert.NotNil(t, w.targets[targetID].LastReportedAt())

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

func TestAgentReportsLockTheTarget(t *testing.T) {
	w := newWorld(t)
	agent := identity.Actor{PrincipalID: agentID}
	require.NoError(t, w.svc.RecordTargetHeartbeat(ctx, agent, org, targetID))
	assert.Equal(t, []string{targetID}, w.lockedTargets)
}

func TestListAssignmentsAsksOnlyForInFlightDeployments(t *testing.T) {
	w := newWorld(t)
	finished := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentHealthy)
	w.deployments[finished.ID()] = finished
	applying := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[applying.ID()] = applying
	items, err := w.svc.ListAssignments(ctx, identity.Actor{PrincipalID: agentID}, org, targetID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, applying.ID(), items[0].Deployment.ID)
}

func TestListAssignments(t *testing.T) {
	w := newWorld(t)
	d := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentApplying)
	w.deployments[d.ID()] = d
	agent := identity.Actor{PrincipalID: agentID}
	items, err := w.svc.ListAssignments(ctx, agent, org, targetID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, d.ID(), items[0].Deployment.ID)
	assert.Contains(t, items[0].HelmReleaseName, "app-")
	require.Len(t, items[0].Artifacts, 1)
}

func TestReleaseChanges(t *testing.T) {
	w := newWorld(t)
	changes, err := w.svc.ReleaseChanges(ctx, ada, org, w.rel.ID(), targetID)
	require.NoError(t, err)
	require.Len(t, changes.Artifacts, 1)
	assert.Nil(t, changes.Artifacts[0].Previous)
	assert.NotEmpty(t, changes.Checks)
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

func mustTarget(org, envID, id, agent string) *delivery.Target {
	t := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: envID, DisplayName: "Prod cluster",
		Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "cluster", delivery.LocatorNamespace: "default"},
	}, &id))
	requireNoErr(t.AssignAgent(agent))
	return t
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

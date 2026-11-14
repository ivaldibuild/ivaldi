package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	org            = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	agentID        = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAG"
	clusterID      = "cls_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	otherClusterID = "cls_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	targetID       = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	otherTargetID  = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	appID          = "app_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

type store struct {
	clusters     map[string]*delivery.Cluster
	targets      map[string]*delivery.Target
	placements   map[string][]*delivery.Placement
	apps         map[string]*delivery.Application
	artifacts    map[string][]*delivery.Artifact
	deployments  map[string]*delivery.Deployment
	assignments  []delivery.DeploymentAssignment
	observed     []delivery.ObserveInput
	observedCtx  []context.Context
	updated      []string
	clusterSaved bool
	// locks records each row locked for update, in order, as "target:<id>"
	// or "cluster:<id>".
	locks []string
}

func newStore() *store {
	return &store{
		clusters: map[string]*delivery.Cluster{}, targets: map[string]*delivery.Target{},
		placements: map[string][]*delivery.Placement{}, apps: map[string]*delivery.Application{},
		artifacts: map[string][]*delivery.Artifact{}, deployments: map[string]*delivery.Deployment{},
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeClusters struct{ *store }

func (f fakeClusters) GetClusterByAgentPrincipalID(_ context.Context, organizationID, principalID string) (*delivery.Cluster, error) {
	for _, c := range f.clusters {
		if c.OrganizationID() == organizationID && c.AgentPrincipalID() != nil && *c.AgentPrincipalID() == principalID && !c.Archived() {
			return c, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (f fakeClusters) GetClusterForUpdate(ctx context.Context, organizationID, id string) (*delivery.Cluster, error) {
	f.locks = append(f.locks, "cluster:"+id)
	c, ok := f.clusters[id]
	if !ok || c.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return c, nil
}

func (f fakeClusters) UpdateCluster(_ context.Context, c *delivery.Cluster) error {
	f.clusters[c.ID()] = c
	f.clusterSaved = true
	return nil
}

type fakeTargets struct{ *store }

func (f fakeTargets) ListTargets(_ context.Context, q delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	var out []*delivery.Target
	for _, t := range f.targets {
		if t.OrganizationID() != q.OrganizationID {
			continue
		}
		if q.ClusterID != "" && t.ClusterID() != q.ClusterID {
			continue
		}
		if q.EnvironmentID != "" && t.EnvironmentID() != q.EnvironmentID {
			continue
		}
		if !q.IncludeArchived && t.Archived() {
			continue
		}
		out = append(out, t)
	}
	return &pagination.Result[*delivery.Target]{Items: out, TotalCount: len(out)}, nil
}

func (f fakeTargets) GetTarget(_ context.Context, organizationID, id string) (*delivery.Target, error) {
	t, ok := f.targets[id]
	if !ok || t.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return t, nil
}

func (f fakeTargets) GetTargetForUpdate(ctx context.Context, organizationID, id string) (*delivery.Target, error) {
	f.locks = append(f.locks, "target:"+id)
	return f.GetTarget(ctx, organizationID, id)
}

func (f fakeTargets) UpdateTarget(_ context.Context, t *delivery.Target) error {
	f.targets[t.ID()] = t
	f.updated = append(f.updated, t.ID())
	return nil
}

type fakeApplications struct{ *store }

func (f fakeApplications) ListPlacementsByTarget(_ context.Context, organizationID, id string) ([]*delivery.Placement, error) {
	var out []*delivery.Placement
	for _, p := range f.placements[id] {
		if p.OrganizationID() == organizationID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f fakeApplications) GetApplication(_ context.Context, organizationID, id string) (*delivery.Application, error) {
	a, ok := f.apps[id]
	if !ok || a.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return a, nil
}

func (f fakeApplications) ListArtifacts(_ context.Context, organizationID, id string, _ bool) ([]*delivery.Artifact, error) {
	var out []*delivery.Artifact
	for _, a := range f.artifacts[id] {
		if a.OrganizationID() == organizationID {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeAssignments struct{ *store }

func (f fakeAssignments) ListClusterAssignments(_ context.Context, _ identity.Actor, organizationID, id string) ([]delivery.DeploymentAssignment, error) {
	if id != clusterID {
		return nil, delivery.ErrNotFound
	}
	return f.assignments, nil
}

type fakeDeployments struct{ *store }

func (f fakeDeployments) GetDeployment(_ context.Context, organizationID, id string) (*delivery.Deployment, error) {
	d, ok := f.deployments[id]
	if !ok || organizationID != org {
		return nil, delivery.ErrNotFound
	}
	return d, nil
}

type fakeObservations struct{ *store }

func (f fakeObservations) Observe(ctx context.Context, _ identity.Actor, in delivery.ObserveInput) (*delivery.DeploymentDetail, error) {
	f.observed = append(f.observed, in)
	f.observedCtx = append(f.observedCtx, ctx)
	return &delivery.DeploymentDetail{}, nil
}

type world struct {
	*store
	svc Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	cluster := must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: org, Slug: "prod-eu", DisplayName: "Prod EU", Runtime: delivery.RuntimeKubernetes,
	}, ptr(clusterID)))
	require.NoError(t, cluster.AssignAgent(agentID))
	other := must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: org, Slug: "other", DisplayName: "Other", Runtime: delivery.RuntimeKubernetes,
	}, ptr(otherClusterID)))
	s.clusters[clusterID], s.clusters[otherClusterID] = cluster, other

	target := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAV", ClusterID: clusterID,
		Slug: "payments", DisplayName: "Payments", Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "prod-eu", delivery.LocatorNamespace: "payments"},
	}, ptr(targetID)))
	otherTarget := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: "env_01ARZ3NDEKTSV4RRFFQ69G5FAV", ClusterID: otherClusterID,
		Slug: "other", DisplayName: "Other", Runtime: delivery.RuntimeKubernetes,
		Locator: map[string]string{delivery.LocatorCluster: "other", delivery.LocatorNamespace: "other"},
	}, ptr(otherTargetID)))
	s.targets[targetID], s.targets[otherTargetID] = target, otherTarget

	app := must(delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: org, WorkspaceID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID: "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV", OwnerTeamID: "tem_01ARZ3NDEKTSV4RRFFQ69G5FAA",
		DisplayName: "Payments",
	}, ptr(appID)))
	placement := must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: org, ApplicationID: appID, TargetID: targetID,
	}))
	artifact := must(delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: org, ApplicationID: appID, Name: "api",
		ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/api",
	}, nil))
	s.apps[appID] = app
	s.placements[targetID] = []*delivery.Placement{placement}
	s.artifacts[appID] = []*delivery.Artifact{artifact}

	svc, err := NewService(
		WithClusters(fakeClusters{s}), WithTargets(fakeTargets{s}), WithApplications(fakeApplications{s}),
		WithDeployments(fakeDeployments{s}),
		WithAssignments(fakeAssignments{s}), WithObservations(fakeObservations{s}), WithTransactor(s),
		WithIntervals(Intervals{Heartbeat: 30 * time.Second, Poll: 5 * time.Second, Watch: 60 * time.Second}),
	)
	require.NoError(t, err)
	return &world{store: s, svc: svc}
}

func ptr[T any](v T) *T { return &v }

func agentCaller() identity.Caller {
	return identity.Caller{
		Actor:          identity.Actor{PrincipalID: agentID, Kind: identity.ActorWorkload},
		OrganizationID: org,
	}
}

func userCaller() identity.Caller {
	return identity.Caller{
		Actor:          identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser},
		OrganizationID: org,
	}
}

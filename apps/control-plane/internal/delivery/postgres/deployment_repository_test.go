package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

func TestDeploymentRepository(t *testing.T) {
	f := newFixture(t)
	at(t, 0)
	builds := NewBuildRepository(f.db)
	releases := NewReleaseRepository(f.db)
	deployments := NewDeploymentRepository(f.db)

	env := f.createEnv(t, orgA, wsA, "prod")
	lc := f.createLifecycle(t, orgA, wsA, "standard", phase("Prod", env.ID()))
	app := f.createApp(t, orgA, wsA, projA, "api")
	api := f.createArtifact(t, orgA, app.ID(), "api")
	main := f.createChannel(t, orgA, app.ID(), lc.ID(), "main", true)
	target := f.createTarget(t, orgA, env.ID(), "prod")

	build := newBuild(orgA, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{}, delivery.BuildCIReported)
	_, err := builds.CreateBuild(f.ctx, build)
	require.NoError(t, err)
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: orgA, ApplicationID: app.ID(), ChannelID: main.ID(), Version: "1.0.0",
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: api.ID(), BuildID: build.ID()}},
	}, nil))
	require.NoError(t, releases.CreateRelease(f.ctx, rel))
	placement := must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: orgA, ApplicationID: app.ID(), TargetID: target.ID(),
		ApplyConfig: &delivery.ApplyConfig{DefinitionArtifact: "chart"},
	}))
	require.NoError(t, f.apps.CreatePlacement(f.ctx, placement))

	d := must(delivery.NewDeployment(delivery.NewDeploymentProps{
		OrganizationID: orgA, ApplicationID: app.ID(), ReleaseID: rel.ID(),
		EnvironmentID: env.ID(), TargetID: target.ID(), Status: release.DeploymentApplying,
	}, nil))
	require.NoError(t, deployments.CreateDeployment(f.ctx, d))

	got, err := deployments.GetDeployment(f.ctx, orgA, d.ID())
	require.NoError(t, err)
	assert.Equal(t, d.Snapshot(), got.Snapshot())
	assert.Empty(t, got.PullEvents(), "a loaded Deployment records no event")

	inFlight, err := deployments.GetInFlightDeployment(f.ctx, orgA, app.ID(), target.ID())
	require.NoError(t, err)
	assert.Equal(t, d.ID(), inFlight.ID())

	page, err := deployments.ListDeployments(f.ctx, delivery.ListDeploymentsQuery{
		OrganizationID: orgA, ApplicationID: app.ID(), Page: pagination.Query{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	for statuses, want := range map[string]int{"applying": 1, "progressing": 0} {
		page, err := deployments.ListDeployments(f.ctx, delivery.ListDeploymentsQuery{
			OrganizationID: orgA, TargetID: target.ID(),
			Statuses: []release.DeploymentStatus{release.DeploymentStatus(statuses), release.DeploymentHealthy},
		})
		require.NoError(t, err)
		assert.Len(t, page.Items, want, "statuses %s", statuses)
	}
	for cluster, want := range map[string]int{target.ClusterID(): 1, f.cluster(t, orgA, "elsewhere").ID(): 0} {
		page, err := deployments.ListDeployments(f.ctx, delivery.ListDeploymentsQuery{OrganizationID: orgA, ClusterID: cluster})
		require.NoError(t, err)
		assert.Len(t, page.Items, want, "deployments on cluster %s's targets", cluster)
	}

	eventStatus := release.DeploymentApplying
	event := must(delivery.NewDeploymentEvent(delivery.NewDeploymentEventProps{
		OrganizationID: orgA, DeploymentID: d.ID(), Kind: delivery.DeploymentEventStatusChanged,
		Status: &eventStatus, ReportID: new("report-1"),
	}, nil))
	inserted, err := deployments.AppendDeploymentEvent(f.ctx, event)
	require.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = deployments.AppendDeploymentEvent(f.ctx, event)
	require.NoError(t, err)
	assert.False(t, inserted, "a report ID is idempotent")
	events, err := deployments.ListDeploymentEvents(f.ctx, orgA, d.ID())
	require.NoError(t, err)
	require.Len(t, events, 1)

	at(t, time.Minute)
	require.NoError(t, d.MoveTo(release.DeploymentHealthy))
	require.NoError(t, deployments.UpdateDeployment(f.ctx, d))
	_, err = deployments.GetInFlightDeployment(f.ctx, orgA, app.ID(), target.ID())
	assert.ErrorIs(t, err, delivery.ErrNotFound)
	envs, err := deployments.ListSucceededEnvironmentIDs(f.ctx, orgA, rel.ID())
	require.NoError(t, err)
	assert.Equal(t, []string{env.ID()}, envs)

	require.NoError(t, placement.SetCurrent(d.ID(), rel.ID()))
	require.NoError(t, f.apps.UpdatePlacementCurrent(f.ctx, placement))
	current, err := f.apps.GetPlacement(f.ctx, orgA, app.ID(), target.ID())
	require.NoError(t, err)
	assert.Equal(t, new(d.ID()), current.CurrentDeploymentID())
	assert.Equal(t, new(rel.ID()), current.CurrentReleaseID())
}

// A rollback replays the Release's latest Deployment to the Target that
// became healthy, and what it recorded survives the round trip.
func TestDeploymentRepositoryFindsTheDeploymentARollbackReplays(t *testing.T) {
	f := newFixture(t)
	builds, releases, deployments := NewBuildRepository(f.db), NewReleaseRepository(f.db), NewDeploymentRepository(f.db)
	env := f.createEnv(t, orgA, wsA, "prod")
	lc := f.createLifecycle(t, orgA, wsA, "standard", phase("Prod", env.ID()))
	app := f.createApp(t, orgA, wsA, projA, "api")
	api := f.createArtifact(t, orgA, app.ID(), "api")
	main := f.createChannel(t, orgA, app.ID(), lc.ID(), "main", true)
	target := f.createTarget(t, orgA, env.ID(), "prod")
	build := newBuild(orgA, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{}, delivery.BuildCIReported)
	_, err := builds.CreateBuild(f.ctx, build)
	require.NoError(t, err)
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: orgA, ApplicationID: app.ID(), ChannelID: main.ID(), Version: "1.0.0",
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: api.ID(), BuildID: build.ID()}},
	}, nil))
	require.NoError(t, releases.CreateRelease(f.ctx, rel))
	require.NoError(t, f.apps.CreatePlacement(f.ctx, must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: orgA, ApplicationID: app.ID(), TargetID: target.ID(),
	}))))

	_, err = deployments.GetLastSucceededDeployment(f.ctx, orgA, rel.ID(), target.ID())
	require.ErrorIs(t, err, delivery.ErrNotFound, "the release has not been healthy there")

	healthy := func(after time.Duration, props delivery.NewDeploymentProps) *delivery.Deployment {
		at(t, after)
		props.OrganizationID, props.ApplicationID, props.ReleaseID = orgA, app.ID(), rel.ID()
		props.EnvironmentID, props.TargetID, props.Status = env.ID(), target.ID(), release.DeploymentApplying
		d := must(delivery.NewDeployment(props, nil))
		require.NoError(t, deployments.CreateDeployment(f.ctx, d))
		require.NoError(t, d.MoveTo(release.DeploymentProgressing))
		require.NoError(t, d.RecordSecretVersions([]delivery.SecretVersion{{
			Name: "Database.Password", StoreID: "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ", Path: "db", Field: "password",
			Version: "41", Fingerprint: "hmac-sha256:v1:ab", DeliveredAs: delivery.VariableDeliverAsSecret,
		}}))
		require.NoError(t, d.MoveTo(release.DeploymentHealthy))
		require.NoError(t, deployments.UpdateDeployment(f.ctx, d))
		return d
	}
	first := healthy(time.Minute, delivery.NewDeploymentProps{})
	second := healthy(2*time.Minute, delivery.NewDeploymentProps{
		Reason: delivery.DeploymentRollback, RestoresDeploymentID: new(first.ID()), SecretsLatest: true,
	})

	last, err := deployments.GetLastSucceededDeployment(f.ctx, orgA, rel.ID(), target.ID())
	require.NoError(t, err)
	assert.Equal(t, second.Snapshot(), last.Snapshot(), "the latest one, with its secret versions and what it restored")
	assert.Equal(t, "41", last.SecretVersions()[0].Version)
}

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

// While a Deployment is requested or approved, withdrawing its Release or
// archiving its Application or Target, which lock the row for update from
// another transaction, waits until it commits.
func TestLockForDeploymentHoldsOffWithdrawalAndArchive(t *testing.T) {
	f := newFixture(t)
	env := f.createEnv(t, orgA, wsA, "prod")
	lc := f.createLifecycle(t, orgA, wsA, "standard", phase("Prod", env.ID()))
	app := f.createApp(t, orgA, wsA, projA, "api")
	api := f.createArtifact(t, orgA, app.ID(), "api")
	main := f.createChannel(t, orgA, app.ID(), lc.ID(), "main", true)
	target := f.createTarget(t, orgA, env.ID(), "prod")
	other := f.createTarget(t, orgA, env.ID(), "other")
	build := newBuild(orgA, "ghcr.io/acme/api", digest("a"), delivery.BuildDetails{}, delivery.BuildCIReported)
	_, err := NewBuildRepository(f.db).CreateBuild(f.ctx, build)
	require.NoError(t, err)
	rel := must(delivery.NewRelease(delivery.NewReleaseProps{
		OrganizationID: orgA, ApplicationID: app.ID(), ChannelID: main.ID(), Version: "1.0.0",
		Builds: []delivery.NewReleaseBuildProps{{ArtifactID: api.ID(), BuildID: build.ID()}},
	}, nil))
	require.NoError(t, NewReleaseRepository(f.db).CreateRelease(f.ctx, rel))

	err = f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
		require.NoError(t, NewDeploymentLocks(f.db).LockForDeployment(ctx, orgA, rel.ID(), []string{target.ID()}))
		f.assertLocked(t, "releases", rel.ID())
		f.assertLocked(t, "applications", app.ID())
		f.assertLocked(t, "targets", target.ID())
		f.assertNotLocked(t, "targets", other.ID())
		return nil
	})
	require.NoError(t, err)
	f.assertNotLocked(t, "releases", rel.ID())
}

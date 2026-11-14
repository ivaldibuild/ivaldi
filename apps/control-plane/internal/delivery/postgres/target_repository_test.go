package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestTargetRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "staging")
	otherEnv := f.createEnv(t, orgA, wsA, "production")
	cluster := f.cluster(t, orgA, "cluster-1")

	t.Run("round trip", func(t *testing.T) {
		tg := must(delivery.NewTarget(delivery.NewTargetProps{
			OrganizationID: orgA,
			EnvironmentID:  env.ID(),
			ClusterID:      cluster.ID(),
			Slug:           "eu-west",
			DisplayName:    "EU West",
			Runtime:        delivery.RuntimeKubernetes,
			Locator:        map[string]string{delivery.LocatorCluster: "cluster-1", delivery.LocatorNamespace: "shop"},
			CanObserve:     true,
		}, nil))
		require.NoError(t, f.targets.CreateTarget(ctx, tg))

		got, err := f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Equal(t, cluster.ID(), got.ClusterID())
		assert.True(t, got.CanObserve())
		assert.False(t, got.CanExecute())

		bySlug, err := f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "eu-west")
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), bySlug.Snapshot())

		byNamespace, err := f.targets.GetTargetByNamespace(ctx, orgA, cluster.ID(), "shop")
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), byNamespace.Snapshot())
	})

	t.Run("an unknown environment or cluster is not found", func(t *testing.T) {
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(cluster.ID(), orgA, id.New("env"), "orphan")), delivery.ErrNotFound)
		foreign := f.createEnv(t, orgB, wsA, "foreign")
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(cluster.ID(), orgA, foreign.ID(), "orphan")), delivery.ErrNotFound, "the environment is in another organization")
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(id.New("cls"), orgA, env.ID(), "orphan")), delivery.ErrNotFound, "the cluster does not exist")
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		tg := f.createTarget(t, orgA, env.ID(), "isolated")
		_, err := f.targets.GetTarget(ctx, orgB, tg.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.targets.GetTargetBySlug(ctx, orgB, env.ID(), "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.targets.GetTargetByNamespace(ctx, orgB, cluster.ID(), "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		page, err := f.targets.ListTargets(ctx, delivery.ListTargetsQuery{OrganizationID: orgB, EnvironmentID: env.ID()})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		n, err := f.targets.CountActiveTargets(ctx, orgB, env.ID())
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		tg := f.createTarget(t, orgA, env.ID(), "to-update")

		at(t, time.Minute)
		require.NoError(t, tg.Update(delivery.UpdateTargetProps{
			Slug:        new("updated"),
			DisplayName: new("Updated"),
			Locator:     map[string]string{delivery.LocatorCluster: "cluster-1", delivery.LocatorNamespace: "moved"},
		}))
		assert.True(t, tg.SetCapabilities(true, true))
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err := f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Equal(t, t0, got.CreatedAt())
		assert.True(t, got.CanObserve())
		assert.True(t, got.CanExecute())
		assert.Equal(t, "moved", got.Locator()[delivery.LocatorNamespace])

		at(t, 2*time.Minute)
		assert.True(t, tg.SetCapabilities(false, false))
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.False(t, got.CanObserve(), "a heartbeat can withdraw a capability")

		at(t, 3*time.Minute)
		tg.Archive()
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		_, err = f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "updated")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived targets are not found by slug")
		_, err = f.targets.GetTargetByNamespace(ctx, orgA, cluster.ID(), "moved")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived targets are not found by namespace")

		at(t, 4*time.Minute)
		tg.Restore()
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.False(t, got.Archived())
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.targets.UpdateTarget(ctx, newTarget(cluster.ID(), orgA, env.ID(), "never-stored")), delivery.ErrNotFound)
	})

	t.Run("slugs are unique among an environment's active targets", func(t *testing.T) {
		first := f.createTarget(t, orgA, env.ID(), "clash")

		// Same slug, another namespace: only the slug is taken.
		duplicate := f.targetWithNamespace(t, orgA, env.ID(), cluster, "clash", "clash-2")
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, duplicate), delivery.ErrSlugTaken)

		// The same slug in another Environment is fine, with its own namespace.
		other := f.targetWithNamespace(t, orgA, otherEnv.ID(), cluster, "clash", "clash-3")
		require.NoError(t, f.targets.CreateTarget(ctx, other))

		first.Archive()
		require.NoError(t, f.targets.UpdateTarget(ctx, first))
		second := f.createTarget(t, orgA, env.ID(), "clash")
		got, err := f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.targets.UpdateTarget(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("a namespace holds one active target per cluster", func(t *testing.T) {
		first := f.createTargetIn(t, orgA, otherEnv.ID(), cluster, "shared")
		second := newTarget(cluster.ID(), orgA, env.ID(), "shared-2")
		secondLocator := second.Locator()
		secondLocator[delivery.LocatorNamespace] = "shared"
		require.NoError(t, second.Update(delivery.UpdateTargetProps{Locator: secondLocator}))
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, second), delivery.ErrConflict, "the duplicate is refused by the unique index")

		// The service turns that conflict into the name of the first Target.
		existing, err := f.targets.GetTargetByNamespace(ctx, orgA, cluster.ID(), "shared")
		require.NoError(t, err)
		assert.Equal(t, first.ID(), existing.ID())

		otherCluster := f.cluster(t, orgA, "cluster-2")
		third := newTarget(otherCluster.ID(), orgA, env.ID(), "shared")
		thirdLocator := third.Locator()
		thirdLocator[delivery.LocatorNamespace] = "shared"
		require.NoError(t, third.Update(delivery.UpdateTargetProps{Locator: thirdLocator}))
		require.NoError(t, f.targets.CreateTarget(ctx, third), "the same namespace in another cluster is fine")
	})
}

// targetWithNamespace builds a Target with a namespace other than its slug.
func (f *fixture) targetWithNamespace(t *testing.T, org, envID string, cluster *delivery.Cluster, slug, namespace string) *delivery.Target {
	t.Helper()
	tg := newTarget(cluster.ID(), org, envID, slug)
	locator := tg.Locator()
	locator[delivery.LocatorNamespace] = namespace
	require.NoError(t, tg.Update(delivery.UpdateTargetProps{Locator: locator}))
	return tg
}

// createTargetIn stores a Target in the given Cluster directly.
func (f *fixture) createTargetIn(t *testing.T, org, envID string, cluster *delivery.Cluster, namespace string) *delivery.Target {
	t.Helper()
	tg := newTarget(cluster.ID(), org, envID, namespace)
	locator := tg.Locator()
	locator[delivery.LocatorNamespace] = namespace
	require.NoError(t, tg.Update(delivery.UpdateTargetProps{Locator: locator}))
	require.NoError(t, f.targets.CreateTarget(f.ctx, tg))
	return tg
}

func TestTargetListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "staging")
	otherEnv := f.createEnv(t, orgA, wsA, "production")
	cluster := f.cluster(t, orgA, "cluster-1")

	first := f.createTarget(t, orgA, env.ID(), "first")
	at(t, time.Second)
	second := f.createTarget(t, orgA, env.ID(), "second")
	at(t, 2*time.Second)
	archived := f.createTarget(t, orgA, env.ID(), "archived")
	archived.Archive()
	require.NoError(t, f.targets.UpdateTarget(ctx, archived))
	f.createTarget(t, orgA, otherEnv.ID(), "elsewhere")

	page, err := f.targets.ListTargets(ctx, delivery.ListTargetsQuery{OrganizationID: orgA, EnvironmentID: env.ID()})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), second.ID()}, idsOf(page.Items))
	assert.Equal(t, 2, page.TotalCount)
	assert.Equal(t, second.Snapshot(), page.Items[1].Snapshot())

	page, err = f.targets.ListTargets(ctx, delivery.ListTargetsQuery{OrganizationID: orgA, EnvironmentID: env.ID(), IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), second.ID(), archived.ID()}, idsOf(page.Items))
	assert.Equal(t, 3, page.TotalCount)

	page, err = f.targets.ListTargets(ctx, delivery.ListTargetsQuery{OrganizationID: orgA, ClusterID: cluster.ID()})
	require.NoError(t, err)
	assert.Len(t, page.Items, 3, "the archived one is left out")
	assert.Equal(t, 3, page.TotalCount)

	n, err := f.targets.CountActiveTargets(ctx, orgA, env.ID())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = f.targets.CountActiveTargets(ctx, orgA, otherEnv.ID())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

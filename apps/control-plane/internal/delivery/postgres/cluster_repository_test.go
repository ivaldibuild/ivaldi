package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

func TestClusterRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		c := newCluster(orgA, "prod-eu")
		at(t, time.Second)
		require.NoError(t, c.AssignAgent("agent-round-trip"))
		require.NoError(t, c.RecordHeartbeat("agent-round-trip", map[string]any{
			"agent_version":      "1.4.0",
			"kubernetes_version": "v1.31.4",
			"deployers":          []any{"helm"},
		}))
		require.NoError(t, c.RecordAgentFailure("identity_in_use: served by prod-eu-2"))
		require.NoError(t, f.clusters.CreateCluster(ctx, c))

		got, err := f.clusters.GetCluster(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.Equal(t, t0.Add(time.Second), *got.LastReportedAt())

		bySlug, err := f.clusters.GetClusterBySlug(ctx, orgA, "prod-eu")
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), bySlug.Snapshot())

		byAgent, err := f.clusters.GetClusterByAgentPrincipalID(ctx, orgA, "agent-round-trip")
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), byAgent.Snapshot())
	})

	t.Run("round trip without an agent", func(t *testing.T) {
		c := f.createCluster(t, orgA, "bare")
		got, err := f.clusters.GetCluster(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.Nil(t, got.AgentPrincipalID())
		assert.Nil(t, got.AgentLastFailure())
		assert.Nil(t, got.LastReportedAt())
		assert.Empty(t, got.AgentCapabilities())
	})

	t.Run("an agent serves one cluster", func(t *testing.T) {
		first := f.createCluster(t, orgA, "first")
		require.NoError(t, first.AssignAgent("agent-shared"))
		require.NoError(t, f.clusters.UpdateCluster(ctx, first))

		second := newCluster(orgA, "second")
		require.NoError(t, second.AssignAgent("agent-shared"))
		assert.ErrorIs(t, f.clusters.CreateCluster(ctx, second), delivery.ErrConflict)
	})

	t.Run("slugs are unique among an organization's active clusters", func(t *testing.T) {
		first := f.createCluster(t, orgA, "clash")
		assert.ErrorIs(t, f.clusters.CreateCluster(ctx, newCluster(orgA, "clash")), delivery.ErrSlugTaken)
		f.createCluster(t, orgB, "clash")

		_ = first.Archive()
		at(t, time.Minute)
		require.NoError(t, f.clusters.UpdateCluster(ctx, first))
		second := f.createCluster(t, orgA, "clash")
		got, err := f.clusters.GetClusterBySlug(ctx, orgA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		at(t, 2*time.Minute)
		assert.ErrorIs(t, f.clusters.UpdateCluster(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		c := f.createCluster(t, orgA, "isolated")
		require.NoError(t, c.AssignAgent("agent-isolated"))
		at(t, time.Minute)
		require.NoError(t, f.clusters.UpdateCluster(ctx, c))

		_, err := f.clusters.GetCluster(ctx, orgB, c.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.clusters.GetClusterBySlug(ctx, orgB, "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.clusters.GetClusterByAgentPrincipalID(ctx, orgB, "agent-isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		page, err := f.clusters.ListClusters(ctx, delivery.ListClustersQuery{OrganizationID: orgB})
		require.NoError(t, err)
		assert.NotContains(t, idsOf(page.Items), c.ID(), "the cluster is not listed in another organization")
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		c := f.createCluster(t, orgA, "to-update")
		at(t, time.Second)
		require.NoError(t, c.AssignAgent("agent-update"))
		require.NoError(t, c.RecordHeartbeat("agent-update", map[string]any{"agent_version": "1.0.0"}))
		require.NoError(t, f.clusters.UpdateCluster(ctx, c))
		got, err := f.clusters.GetCluster(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.Equal(t, t0, got.CreatedAt())

		at(t, 2*time.Minute)
		require.NoError(t, c.Update(delivery.UpdateClusterProps{DisplayName: new("Updated")}))
		_ = c.Archive()
		require.NoError(t, f.clusters.UpdateCluster(ctx, c))
		got, err = f.clusters.GetCluster(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.True(t, got.Archived())
		assert.Nil(t, got.AgentPrincipalID(), "archiving clears the agent")
		assert.Empty(t, got.AgentCapabilities())
		_, err = f.clusters.GetClusterBySlug(ctx, orgA, "to-update")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived clusters are not found by slug")

		at(t, 3*time.Minute)
		c.Restore()
		require.NoError(t, f.clusters.UpdateCluster(ctx, c))
		got, err = f.clusters.GetCluster(ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())
		assert.False(t, got.Archived())
		assert.Equal(t, "Updated", got.DisplayName())
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.clusters.UpdateCluster(ctx, newCluster(orgA, "never-stored")), delivery.ErrNotFound)
	})
}

func TestClusterForUpdate(t *testing.T) {
	f := newFixture(t)
	c := f.createCluster(t, orgA, "locked")

	t.Run("locks the row and commits", func(t *testing.T) {
		at(t, time.Minute)
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.clusters.GetClusterForUpdate(ctx, orgA, c.ID())
			require.NoError(t, err)
			assert.Equal(t, c.Snapshot(), got.Snapshot())
			f.assertLocked(t, "clusters", c.ID())

			require.NoError(t, got.Update(delivery.UpdateClusterProps{DisplayName: new("Locked and updated")}))
			return f.clusters.UpdateCluster(ctx, got)
		})
		require.NoError(t, err)
		f.assertNotLocked(t, "clusters", c.ID())

		got, err := f.clusters.GetCluster(f.ctx, orgA, c.ID())
		require.NoError(t, err)
		assert.Equal(t, "Locked and updated", got.DisplayName())
	})

	t.Run("a plain get does not lock", func(t *testing.T) {
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			_, err := f.clusters.GetCluster(ctx, orgA, c.ID())
			require.NoError(t, err)
			f.assertNotLocked(t, "clusters", c.ID())
			return nil
		})
		require.NoError(t, err)
	})
}

func TestClusterList(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	first := f.createCluster(t, orgA, "first")
	at(t, time.Second)
	second := f.createCluster(t, orgA, "second")
	at(t, 2*time.Second)
	archived := f.createCluster(t, orgA, "archived")
	_ = archived.Archive()
	require.NoError(t, f.clusters.UpdateCluster(ctx, archived))
	f.createCluster(t, orgB, "elsewhere")

	page, err := f.clusters.ListClusters(ctx, delivery.ListClustersQuery{OrganizationID: orgA})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), second.ID()}, idsOf(page.Items))
	assert.Equal(t, 2, page.TotalCount)

	page, err = f.clusters.ListClusters(ctx, delivery.ListClustersQuery{OrganizationID: orgA, IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), second.ID(), archived.ID()}, idsOf(page.Items))
	assert.Equal(t, 3, page.TotalCount)

	page, err = f.clusters.ListClusters(ctx, delivery.ListClustersQuery{
		OrganizationID: orgA,
		Page:           pagination.Query{SortBy: "slug", SortOrder: "desc"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{second.ID(), first.ID()}, idsOf(page.Items))
}

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

	t.Run("round trip", func(t *testing.T) {
		tg := must(delivery.NewTarget(delivery.NewTargetProps{
			OrganizationID:   orgA,
			EnvironmentID:    env.ID(),
			Slug:             "eu-west",
			DisplayName:      "EU West",
			Runtime:          delivery.RuntimeKubernetes,
			Locator:          map[string]string{delivery.LocatorCluster: "arn:aws:eks:eu-west-1:1:cluster/main", delivery.LocatorNamespace: "shop"},
			AgentPrincipalID: new(agentA),
		}, nil))
		at(t, time.Second)
		require.NoError(t, tg.RecordReport(agentA))
		require.NotNil(t, tg.LastReportedAt())
		require.NoError(t, f.targets.CreateTarget(ctx, tg))

		got, err := f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Equal(t, t0.Add(time.Second), *got.LastReportedAt())

		bySlug, err := f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "eu-west")
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip without an agent", func(t *testing.T) {
		tg := f.createTarget(t, orgA, env.ID(), "bare")
		got, err := f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Nil(t, got.AgentPrincipalID())
		assert.Nil(t, got.LastReportedAt())
	})

	t.Run("an unknown environment is not found", func(t *testing.T) {
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(orgA, id.New("env"), "orphan")), delivery.ErrNotFound)
		foreign := f.createEnv(t, orgB, wsA, "foreign")
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(orgA, foreign.ID(), "orphan")), delivery.ErrNotFound, "the environment is in another organization")
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		tg := f.createTarget(t, orgA, env.ID(), "isolated")
		_, err := f.targets.GetTarget(ctx, orgB, tg.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.targets.GetTargetBySlug(ctx, orgB, env.ID(), "isolated")
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
			Locator:     map[string]string{delivery.LocatorCluster: "cluster-2", delivery.LocatorNamespace: "moved"},
		}))
		require.NoError(t, tg.AssignAgent(agentB))
		require.NoError(t, tg.RecordReport(agentB))
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err := f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Equal(t, t0, got.CreatedAt())

		at(t, 2*time.Minute)
		tg.UnassignAgent()
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.Nil(t, got.AgentPrincipalID(), "unassigning clears the agent")
		assert.Nil(t, got.LastReportedAt())

		at(t, 3*time.Minute)
		tg.Archive()
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		_, err = f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "updated")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived targets are not found by slug")

		at(t, 4*time.Minute)
		tg.Restore()
		require.NoError(t, f.targets.UpdateTarget(ctx, tg))
		got, err = f.targets.GetTarget(ctx, orgA, tg.ID())
		require.NoError(t, err)
		assert.Equal(t, tg.Snapshot(), got.Snapshot())
		assert.False(t, got.Archived())
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.targets.UpdateTarget(ctx, newTarget(orgA, env.ID(), "never-stored")), delivery.ErrNotFound)
	})

	t.Run("slugs are unique among an environment's active targets", func(t *testing.T) {
		first := f.createTarget(t, orgA, env.ID(), "clash")
		assert.ErrorIs(t, f.targets.CreateTarget(ctx, newTarget(orgA, env.ID(), "clash")), delivery.ErrSlugTaken)
		f.createTarget(t, orgA, otherEnv.ID(), "clash")

		first.Archive()
		require.NoError(t, f.targets.UpdateTarget(ctx, first))
		second := f.createTarget(t, orgA, env.ID(), "clash")
		got, err := f.targets.GetTargetBySlug(ctx, orgA, env.ID(), "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.targets.UpdateTarget(ctx, first), delivery.ErrSlugTaken)
	})
}

func TestTargetListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "staging")
	otherEnv := f.createEnv(t, orgA, wsA, "production")

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

	n, err := f.targets.CountActiveTargets(ctx, orgA, env.ID())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = f.targets.CountActiveTargets(ctx, orgA, otherEnv.ID())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

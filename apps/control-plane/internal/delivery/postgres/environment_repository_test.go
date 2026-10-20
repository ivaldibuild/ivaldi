package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestEnvironmentRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		env := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID:   orgA,
			WorkspaceID:      wsA,
			Slug:             "production",
			DisplayName:      "Production",
			Tier:             delivery.TierProduction,
			ExecutionEnabled: true,
		}, nil))
		require.NoError(t, f.envs.CreateEnvironment(ctx, env))

		got, err := f.envs.GetEnvironment(ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got.Snapshot())
		assert.Equal(t, t0, got.CreatedAt(), "microseconds survive")
		assert.Equal(t, time.UTC, got.CreatedAt().Location())

		bySlug, err := f.envs.GetEnvironmentBySlug(ctx, orgA, wsA, "production")
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), bySlug.Snapshot())
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		env := f.createEnv(t, orgA, wsA, "isolated")

		_, err := f.envs.GetEnvironment(ctx, orgB, env.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.envs.GetEnvironmentForUpdate(ctx, orgB, env.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.envs.GetEnvironmentBySlug(ctx, orgB, wsA, "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		got, err := f.envs.GetEnvironments(ctx, orgB, []string{env.ID()})
		require.NoError(t, err)
		assert.Empty(t, got)

		// The same workspace ID and slug in another organization is another record.
		other := f.createEnv(t, orgB, wsA, "isolated")
		page, err := f.envs.ListEnvironments(ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgB, WorkspaceID: wsA})
		require.NoError(t, err)
		assert.Equal(t, []string{other.ID()}, idsOf(page.Items))
		assert.Equal(t, 1, page.TotalCount)

		otherUpdate := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: orgB, WorkspaceID: wsA, Slug: "hijack", DisplayName: "Hijack", Tier: delivery.TierOther,
		}, new(env.ID())))
		assert.ErrorIs(t, f.envs.UpdateEnvironment(ctx, otherUpdate), delivery.ErrNotFound, "an update is scoped to its organization")
		got1, err := f.envs.GetEnvironment(ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got1.Snapshot())
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := f.envs.GetEnvironment(ctx, orgA, id.New("env"))
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.envs.GetEnvironmentBySlug(ctx, orgA, wsA, "missing")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("get environments", func(t *testing.T) {
		a := f.createEnv(t, orgA, wsB, "many-a")
		b := f.createEnv(t, orgA, wsB, "many-b")
		f.createEnv(t, orgA, wsB, "many-c")

		got, err := f.envs.GetEnvironments(ctx, orgA, []string{a.ID(), b.ID(), id.New("env")})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{a.ID(), b.ID()}, idsOf(got))
		for _, e := range got {
			want := map[string]*delivery.Environment{a.ID(): a, b.ID(): b}[e.ID()]
			assert.Equal(t, want.Snapshot(), e.Snapshot())
		}

		none, err := f.envs.GetEnvironments(ctx, orgA, nil)
		require.NoError(t, err)
		assert.Empty(t, none)
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		env := f.createEnv(t, orgA, wsA, "to-update")

		at(t, time.Minute)
		require.NoError(t, env.Update(delivery.UpdateEnvironmentProps{
			Slug:             new("updated"),
			DisplayName:      new("Updated"),
			Tier:             new(delivery.TierDevelopment),
			ExecutionEnabled: new(true),
		}))
		require.NoError(t, f.envs.UpdateEnvironment(ctx, env))
		got, err := f.envs.GetEnvironment(ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got.Snapshot())
		assert.Equal(t, t0, got.CreatedAt(), "created_at is not rewritten")

		at(t, 2*time.Minute)
		env.Archive()
		require.NoError(t, f.envs.UpdateEnvironment(ctx, env))
		got, err = f.envs.GetEnvironment(ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got.Snapshot())
		assert.True(t, got.Archived())
		_, err = f.envs.GetEnvironmentBySlug(ctx, orgA, wsA, "updated")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived environments are not found by slug")

		at(t, 3*time.Minute)
		env.Restore()
		require.NoError(t, f.envs.UpdateEnvironment(ctx, env))
		got, err = f.envs.GetEnvironment(ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got.Snapshot())
		assert.Nil(t, got.DeletedAt(), "restore clears deleted_at")
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.envs.UpdateEnvironment(ctx, newEnv(orgA, wsA, "never-stored")), delivery.ErrNotFound)
	})

	t.Run("slugs are unique among active environments", func(t *testing.T) {
		first := f.createEnv(t, orgA, wsA, "clash")
		err := f.envs.CreateEnvironment(ctx, newEnv(orgA, wsA, "clash"))
		assert.ErrorIs(t, err, delivery.ErrSlugTaken)

		// Another workspace may use it.
		f.createEnv(t, orgA, wsB, "clash")

		first.Archive()
		require.NoError(t, f.envs.UpdateEnvironment(ctx, first))
		second := f.createEnv(t, orgA, wsA, "clash")

		first.Restore()
		assert.ErrorIs(t, f.envs.UpdateEnvironment(ctx, first), delivery.ErrSlugTaken, "restoring onto a taken slug")

		other := f.createEnv(t, orgA, wsA, "clash-other")
		require.NoError(t, other.Update(delivery.UpdateEnvironmentProps{Slug: new("clash")}))
		assert.ErrorIs(t, f.envs.UpdateEnvironment(ctx, other), delivery.ErrSlugTaken, "renaming onto a taken slug")

		got, err := f.envs.GetEnvironmentBySlug(ctx, orgA, wsA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())
	})

	t.Run("a duplicate ID conflicts", func(t *testing.T) {
		env := f.createEnv(t, orgA, wsA, "dup-id")
		dup := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: orgA, WorkspaceID: wsA, Slug: "dup-id-2", DisplayName: "Dup", Tier: delivery.TierOther,
		}, new(env.ID())))
		assert.ErrorIs(t, f.envs.CreateEnvironment(ctx, dup), delivery.ErrConflict)
	})
}

func TestEnvironmentListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	active := f.createEnv(t, orgA, wsA, "active")
	at(t, time.Second)
	archived := f.createEnv(t, orgA, wsA, "archived")
	archived.Archive()
	require.NoError(t, f.envs.UpdateEnvironment(ctx, archived))
	f.createEnv(t, orgA, wsB, "other-workspace")
	f.createEnv(t, orgB, wsA, "other-org")

	page, err := f.envs.ListEnvironments(ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgA, WorkspaceID: wsA})
	require.NoError(t, err)
	assert.Equal(t, []string{active.ID()}, idsOf(page.Items))
	assert.Equal(t, 1, page.TotalCount)
	assert.Equal(t, active.Snapshot(), page.Items[0].Snapshot())

	page, err = f.envs.ListEnvironments(ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgA, WorkspaceID: wsA, IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{active.ID(), archived.ID()}, idsOf(page.Items))
	assert.Equal(t, 2, page.TotalCount)
	assert.Equal(t, archived.Snapshot(), page.Items[1].Snapshot())

	n, err := f.envs.CountActiveEnvironments(ctx, orgA, wsA)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = f.envs.CountActiveEnvironments(ctx, orgB, wsB)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestEnvironmentForUpdate(t *testing.T) {
	f := newFixture(t)
	env := f.createEnv(t, orgA, wsA, "locked")

	t.Run("locks the row and commits", func(t *testing.T) {
		at(t, time.Minute)
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.envs.GetEnvironmentForUpdate(ctx, orgA, env.ID())
			require.NoError(t, err)
			assert.Equal(t, env.Snapshot(), got.Snapshot())
			f.assertLocked(t, "environments", env.ID())

			require.NoError(t, got.Update(delivery.UpdateEnvironmentProps{DisplayName: new("Locked and updated")}))
			return f.envs.UpdateEnvironment(ctx, got)
		})
		require.NoError(t, err)
		f.assertNotLocked(t, "environments", env.ID())

		got, err := f.envs.GetEnvironment(f.ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, "Locked and updated", got.DisplayName())
		env = got
	})

	t.Run("a plain get does not lock", func(t *testing.T) {
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			_, err := f.envs.GetEnvironment(ctx, orgA, env.ID())
			require.NoError(t, err)
			f.assertNotLocked(t, "environments", env.ID())
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("a rollback discards the update", func(t *testing.T) {
		stop := errors.New("stop")
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.envs.GetEnvironmentForUpdate(ctx, orgA, env.ID())
			require.NoError(t, err)
			require.NoError(t, got.Update(delivery.UpdateEnvironmentProps{DisplayName: new("Rolled back")}))
			require.NoError(t, f.envs.UpdateEnvironment(ctx, got))
			require.NoError(t, f.envs.CreateEnvironment(ctx, newEnv(orgA, wsA, "rolled-back")))
			return stop
		})
		require.ErrorIs(t, err, stop)

		got, err := f.envs.GetEnvironment(f.ctx, orgA, env.ID())
		require.NoError(t, err)
		assert.Equal(t, env.Snapshot(), got.Snapshot())
		_, err = f.envs.GetEnvironmentBySlug(f.ctx, orgA, wsA, "rolled-back")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
	})
}

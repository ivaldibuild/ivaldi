package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// lifecycleEnvs creates n environments in orgA's wsA.
func (f *fixture) lifecycleEnvs(t *testing.T, prefix string, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range n {
		out[i] = f.createEnv(t, orgA, wsA, prefix+string(rune('a'+i))).ID()
	}
	return out
}

// phaseRows returns the stored phases of a lifecycle as "position:name".
func (f *fixture) phaseRows(t *testing.T, lifecycleID string) []string {
	t.Helper()
	var rows []struct {
		Position int
		Name     string
	}
	require.NoError(t, f.db.Raw("SELECT position, name FROM delivery.lifecycle_phases WHERE lifecycle_id = ? ORDER BY position", lifecycleID).Scan(&rows).Error)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = string(rune('0'+r.Position)) + ":" + r.Name
	}
	return out
}

func TestLifecycleRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	envs := f.lifecycleEnvs(t, "env-", 5)

	t.Run("round trip", func(t *testing.T) {
		// Environment IDs are given out of order; the phase sorts them.
		unsorted := []string{envs[2], envs[0]}
		l := must(delivery.NewLifecycle(delivery.NewLifecycleProps{
			OrganizationID: orgA,
			WorkspaceID:    wsA,
			Slug:           "mainline",
			DisplayName:    "Mainline",
			Description:    new("The usual path"),
			Phases: []delivery.NewLifecyclePhaseProps{
				{Name: "Test", Optional: true, MinEnvironments: new(1), EnvironmentIDs: unsorted},
				{Name: "Staging", EnvironmentIDs: []string{envs[1]}},
				{Name: "Production", EnvironmentIDs: []string{envs[4], envs[3]}},
			},
		}, nil))
		require.NoError(t, f.lifecycles.CreateLifecycle(ctx, l))

		got, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		phases := got.Phases()
		require.Len(t, phases, 3)
		assert.Equal(t, []string{"Test", "Staging", "Production"}, []string{phases[0].Name, phases[1].Name, phases[2].Name})
		for i, p := range phases {
			assert.Equal(t, i, p.Position)
			assert.True(t, slices.IsSorted(p.EnvironmentIDs), "phase %d environments are sorted", i)
		}
		assert.Equal(t, new(1), phases[0].MinEnvironments)
		assert.Nil(t, phases[1].MinEnvironments)

		bySlug, err := f.lifecycles.GetLifecycleBySlug(ctx, orgA, wsA, "mainline")
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip without phases or description", func(t *testing.T) {
		l := f.createLifecycle(t, orgA, wsA, "empty")
		got, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.Empty(t, got.Phases())
		assert.Nil(t, got.Description())
	})

	t.Run("an unknown environment is not found and nothing is stored", func(t *testing.T) {
		l := newLifecycle(orgA, wsA, "unknown-env", phase("One", envs[0]), phase("Two", id.New("env")))
		assert.ErrorIs(t, f.lifecycles.CreateLifecycle(ctx, l), delivery.ErrNotFound)
		_, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound, "the lifecycle row rolled back with its phases")
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phases WHERE lifecycle_id = ?", l.ID()))
	})

	t.Run("an environment of another organization is not found", func(t *testing.T) {
		foreign := f.createEnv(t, orgB, wsA, "foreign")
		l := newLifecycle(orgA, wsA, "foreign-env", phase("One", foreign.ID()))
		assert.ErrorIs(t, f.lifecycles.CreateLifecycle(ctx, l), delivery.ErrNotFound)
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		l := f.createLifecycle(t, orgA, wsA, "isolated", phase("One", envs[0]))
		_, err := f.lifecycles.GetLifecycle(ctx, orgB, l.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.lifecycles.GetLifecycleForUpdate(ctx, orgB, l.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.lifecycles.GetLifecycleBySlug(ctx, orgB, wsA, "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)

		page, err := f.lifecycles.ListLifecycles(ctx, delivery.ListLifecyclesQuery{OrganizationID: orgB, WorkspaceID: wsA})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		assert.Zero(t, page.TotalCount)

		n, err := f.lifecycles.CountActiveLifecyclesWithEnvironment(ctx, orgB, envs[0])
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("slugs are unique among active lifecycles", func(t *testing.T) {
		first := f.createLifecycle(t, orgA, wsA, "clash")
		assert.ErrorIs(t, f.lifecycles.CreateLifecycle(ctx, newLifecycle(orgA, wsA, "clash")), delivery.ErrSlugTaken)
		f.createLifecycle(t, orgA, wsB, "clash")

		first.Archive()
		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, first))
		_, err := f.lifecycles.GetLifecycleBySlug(ctx, orgA, wsA, "clash")
		assert.ErrorIs(t, err, delivery.ErrNotFound, "archived lifecycles are not found by slug")
		second := f.createLifecycle(t, orgA, wsA, "clash")

		got, err := f.lifecycles.GetLifecycleBySlug(ctx, orgA, wsA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.lifecycles.UpdateLifecycle(ctx, first), delivery.ErrSlugTaken)
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.lifecycles.UpdateLifecycle(ctx, newLifecycle(orgA, wsA, "never-stored", phase("One", envs[0]))), delivery.ErrNotFound)
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phases WHERE name = 'One' AND lifecycle_id NOT IN (SELECT id FROM delivery.lifecycles)"))
	})
}

func TestLifecycleUpdatePhases(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	envs := f.lifecycleEnvs(t, "env-", 6)

	l := f.createLifecycle(t, orgA, wsA, "phased",
		phase("A", envs[0], envs[1]),
		phase("B", envs[2]),
		phase("C", envs[3]),
	)
	original := l.Phases()
	idA, idB, idC := original[0].ID, original[1].ID, original[2].ID

	t.Run("add, update, reorder, remove, and move an environment at once", func(t *testing.T) {
		at(t, time.Hour)
		require.NoError(t, l.Update(delivery.UpdateLifecycleProps{DisplayName: new("Phased"), Description: new("Changed")}))
		// Swap A and B.
		require.NoError(t, l.MovePhase(idA, 1))
		// Move envs[1] from A to B.
		require.NoError(t, l.UpdatePhase(idA, delivery.LifecyclePhaseProps{Name: "A2", Optional: true, EnvironmentIDs: []string{envs[0]}}))
		require.NoError(t, l.UpdatePhase(idB, delivery.LifecyclePhaseProps{Name: "B", MinEnvironments: new(1), EnvironmentIDs: []string{envs[2], envs[1]}}))
		require.NoError(t, l.RemovePhase(idC))
		added, err := l.AddPhase(delivery.LifecyclePhaseProps{Name: "D", EnvironmentIDs: []string{envs[3], envs[4]}})
		require.NoError(t, err)

		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, l))

		got, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), got.Snapshot())

		phases := got.Phases()
		require.Len(t, phases, 3)
		assert.Equal(t, []string{idB, idA, added.ID}, []string{phases[0].ID, phases[1].ID, phases[2].ID}, "kept phases keep their IDs")
		assert.Equal(t, original[1].CreatedAt, phases[0].CreatedAt, "kept phases keep their creation time")
		assert.Equal(t, original[0].CreatedAt, phases[1].CreatedAt)
		assert.Equal(t, t0.Add(time.Hour), phases[1].UpdatedAt)
		assert.Equal(t, t0.Add(time.Hour), phases[2].CreatedAt)
		assert.Equal(t, sortedCopy(envs[1], envs[2]), phases[0].EnvironmentIDs, "envs[1] moved from A to B")
		assert.Equal(t, []string{envs[0]}, phases[1].EnvironmentIDs)
		assert.Equal(t, []string{"0:B", "1:A2", "2:D"}, f.phaseRows(t, l.ID()))

		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phases WHERE id = ?", idC), "the removed phase is deleted")
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phase_environments WHERE phase_id = ?", idC), "and its environments")
		assert.Equal(t, 5, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phase_environments WHERE lifecycle_id = ?", l.ID()))
	})

	t.Run("swap two phases", func(t *testing.T) {
		at(t, 2*time.Hour)
		phases := l.Phases()
		require.NoError(t, l.MovePhase(phases[2].ID, 0))
		require.NoError(t, l.MovePhase(phases[0].ID, 2))
		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, l))

		got, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.Equal(t, []string{"0:D", "1:A2", "2:B"}, f.phaseRows(t, l.ID()))
	})

	t.Run("a failed update changes nothing", func(t *testing.T) {
		before, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)

		at(t, 3*time.Hour)
		changed := must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID()))
		require.NoError(t, changed.Update(delivery.UpdateLifecycleProps{DisplayName: new("Broken")}))
		require.NoError(t, changed.RemovePhase(changed.Phases()[0].ID))
		_, err = changed.AddPhase(delivery.LifecyclePhaseProps{Name: "Unknown", EnvironmentIDs: []string{id.New("env")}})
		require.NoError(t, err)
		assert.ErrorIs(t, f.lifecycles.UpdateLifecycle(ctx, changed), delivery.ErrNotFound)

		got, err := f.lifecycles.GetLifecycle(ctx, orgA, l.ID())
		require.NoError(t, err)
		assert.Equal(t, before.Snapshot(), got.Snapshot(), "the update rolled back")
	})

	t.Run("remove every phase", func(t *testing.T) {
		other := f.createLifecycle(t, orgA, wsA, "to-empty", phase("X", envs[5]))
		require.NoError(t, other.RemovePhase(other.Phases()[0].ID))
		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, other))

		got, err := f.lifecycles.GetLifecycle(ctx, orgA, other.ID())
		require.NoError(t, err)
		assert.Equal(t, other.Snapshot(), got.Snapshot())
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phases WHERE lifecycle_id = ?", other.ID()))
		assert.Zero(t, f.count(t, "SELECT count(*) FROM delivery.lifecycle_phase_environments WHERE lifecycle_id = ?", other.ID()))

		// The other lifecycle's phases are untouched.
		assert.Len(t, must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID())).Phases(), 3)
	})

	t.Run("archive and restore", func(t *testing.T) {
		at(t, 4*time.Hour)
		l = must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID()))
		l.Archive()
		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, l))
		got := must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID()))
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.True(t, got.Archived())
		assert.Len(t, got.Phases(), 3, "archiving keeps the phases")

		at(t, 5*time.Hour)
		l.Restore()
		require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, l))
		got = must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID()))
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.False(t, got.Archived())
	})
}

func TestLifecycleUpdateInTransaction(t *testing.T) {
	f := newFixture(t)
	envs := f.lifecycleEnvs(t, "env-", 3)
	l := f.createLifecycle(t, orgA, wsA, "tx", phase("A", envs[0]), phase("B", envs[1]))

	t.Run("joins the transaction and commits", func(t *testing.T) {
		at(t, time.Minute)
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.lifecycles.GetLifecycleForUpdate(ctx, orgA, l.ID())
			require.NoError(t, err)
			assert.Equal(t, l.Snapshot(), got.Snapshot())
			f.assertLocked(t, "lifecycles", l.ID())

			phases := got.Phases()
			require.NoError(t, got.MovePhase(phases[1].ID, 0))
			require.NoError(t, got.UpdatePhase(phases[0].ID, delivery.LifecyclePhaseProps{Name: "A", EnvironmentIDs: []string{envs[2]}}))
			require.NoError(t, got.UpdatePhase(phases[1].ID, delivery.LifecyclePhaseProps{Name: "B", EnvironmentIDs: []string{envs[0], envs[1]}}))
			if err := f.lifecycles.UpdateLifecycle(ctx, got); err != nil {
				return err
			}
			// Not visible outside the transaction until it commits.
			outside := must(f.lifecycles.GetLifecycle(f.ctx, orgA, l.ID()))
			assert.Equal(t, l.Snapshot(), outside.Snapshot())
			l = got
			return nil
		})
		require.NoError(t, err)

		got := must(f.lifecycles.GetLifecycle(f.ctx, orgA, l.ID()))
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.Equal(t, []string{"0:B", "1:A"}, f.phaseRows(t, l.ID()))
	})

	t.Run("a rollback discards the update", func(t *testing.T) {
		at(t, 2*time.Minute)
		stop := errors.New("stop")
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got := must(f.lifecycles.GetLifecycleForUpdate(ctx, orgA, l.ID()))
			require.NoError(t, got.RemovePhase(got.Phases()[0].ID))
			require.NoError(t, got.Update(delivery.UpdateLifecycleProps{Slug: new("renamed")}))
			require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, got))
			assert.Len(t, must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID())).Phases(), 1, "the transaction sees its own write")
			return stop
		})
		require.ErrorIs(t, err, stop)

		got := must(f.lifecycles.GetLifecycle(f.ctx, orgA, l.ID()))
		assert.Equal(t, l.Snapshot(), got.Snapshot())
		assert.Equal(t, []string{"0:B", "1:A"}, f.phaseRows(t, l.ID()))
	})

	t.Run("a failed update inside a transaction leaves it usable", func(t *testing.T) {
		at(t, 3*time.Minute)
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			bad := must(f.lifecycles.GetLifecycleForUpdate(ctx, orgA, l.ID()))
			_, err := bad.AddPhase(delivery.LifecyclePhaseProps{Name: "Unknown", EnvironmentIDs: []string{id.New("env")}})
			require.NoError(t, err)
			require.ErrorIs(t, f.lifecycles.UpdateLifecycle(ctx, bad), delivery.ErrNotFound)

			good := must(f.lifecycles.GetLifecycle(ctx, orgA, l.ID()))
			require.NoError(t, good.Update(delivery.UpdateLifecycleProps{DisplayName: new("After a failure")}))
			return f.lifecycles.UpdateLifecycle(ctx, good)
		})
		require.NoError(t, err)
		got := must(f.lifecycles.GetLifecycle(f.ctx, orgA, l.ID()))
		assert.Equal(t, "After a failure", got.DisplayName())
		assert.Len(t, got.Phases(), 2)
	})
}

func TestLifecycleListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	envs := f.lifecycleEnvs(t, "env-", 3)

	first := f.createLifecycle(t, orgA, wsA, "first", phase("A", envs[0]), phase("B", envs[1]))
	at(t, time.Second)
	second := f.createLifecycle(t, orgA, wsA, "second", phase("A", envs[0]))
	at(t, 2*time.Second)
	archived := f.createLifecycle(t, orgA, wsA, "archived", phase("A", envs[0], envs[2]))
	archived.Archive()
	require.NoError(t, f.lifecycles.UpdateLifecycle(ctx, archived))
	f.createLifecycle(t, orgA, wsB, "other-workspace")

	page, err := f.lifecycles.ListLifecycles(ctx, delivery.ListLifecyclesQuery{OrganizationID: orgA, WorkspaceID: wsA})
	require.NoError(t, err)
	require.Equal(t, []string{first.ID(), second.ID()}, idsOf(page.Items))
	assert.Equal(t, first.Snapshot(), page.Items[0].Snapshot(), "listed lifecycles carry their phases")
	assert.Equal(t, second.Snapshot(), page.Items[1].Snapshot())
	assert.Equal(t, 2, page.TotalCount)

	page, err = f.lifecycles.ListLifecycles(ctx, delivery.ListLifecyclesQuery{OrganizationID: orgA, WorkspaceID: wsA, IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), second.ID(), archived.ID()}, idsOf(page.Items))
	assert.Equal(t, archived.Snapshot(), page.Items[2].Snapshot())

	n, err := f.lifecycles.CountActiveLifecycles(ctx, orgA, wsA)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, tt := range []struct {
		env  string
		want int
	}{
		{envs[0], 2}, // in first and second; archived is not counted
		{envs[1], 1},
		{envs[2], 0}, // only in the archived one
		{id.New("env"), 0},
	} {
		n, err := f.lifecycles.CountActiveLifecyclesWithEnvironment(ctx, orgA, tt.env)
		require.NoError(t, err)
		assert.Equal(t, tt.want, n, "lifecycles with %s", tt.env)
	}
}

func sortedCopy(s ...string) []string {
	return slices.Sorted(slices.Values(s))
}

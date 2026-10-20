package postgres

import (
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// pagedEnvs creates seven environments in orgA's wsA, a second apart, whose
// slug and display name orders both differ from their creation order, plus
// rows the list must leave out. It returns the IDs in creation order and
// maps from ID to slug and display name.
func pagedEnvs(t *testing.T, f *fixture) (created []string, slugOf, nameOf map[string]string) {
	t.Helper()
	slugs := []string{"delta", "alpha", "golf", "charlie", "echo", "bravo", "foxtrot"}
	names := []string{"Mike", "Kilo", "Zulu", "Lima", "Oscar", "November", "Juliet"}
	slugOf, nameOf = map[string]string{}, map[string]string{}
	for i := range slugs {
		at(t, time.Duration(i)*time.Second)
		e := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: orgA, WorkspaceID: wsA, Slug: slugs[i], DisplayName: names[i], Tier: delivery.TierOther,
		}, nil))
		require.NoError(t, f.envs.CreateEnvironment(f.ctx, e))
		created = append(created, e.ID())
		slugOf[e.ID()], nameOf[e.ID()] = slugs[i], names[i]
	}
	at(t, time.Minute)
	archived := f.createEnv(t, orgA, wsA, "archived")
	archived.Archive()
	require.NoError(t, f.envs.UpdateEnvironment(f.ctx, archived))
	f.createEnv(t, orgA, wsB, "other-workspace")
	f.createEnv(t, orgB, wsA, "other-org")
	return created, slugOf, nameOf
}

func sortedBy(ids []string, key map[string]string) []string {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b string) int {
		if key[a] < key[b] {
			return -1
		}
		if key[a] > key[b] {
			return 1
		}
		return 0
	})
	return out
}

func reversed(s []string) []string {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

func TestEnvironmentPagination(t *testing.T) {
	f := newFixture(t)
	created, slugOf, nameOf := pagedEnvs(t, f)
	list := func(q pagination.Query) (*pagination.Result[*delivery.Environment], error) {
		return f.envs.ListEnvironments(f.ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgA, WorkspaceID: wsA, Page: q})
	}

	t.Run("default is everything by creation time", func(t *testing.T) {
		res, err := list(pagination.Query{})
		require.NoError(t, err)
		assert.Equal(t, created, idsOf(res.Items))
		assert.Equal(t, 7, res.TotalCount)
		assert.False(t, res.HasNext)
		assert.False(t, res.HasPrevious)
		assert.Nil(t, res.NextCursor)
		assert.Nil(t, res.PreviousCursor)
	})

	t.Run("limit", func(t *testing.T) {
		res, err := list(pagination.Query{Limit: 3})
		require.NoError(t, err)
		assert.Equal(t, created[:3], idsOf(res.Items))
		assert.Equal(t, 7, res.TotalCount, "the total counts every page")
		assert.True(t, res.HasNext)
		assert.False(t, res.HasPrevious)
		assert.NotNil(t, res.NextCursor)
		assert.Nil(t, res.PreviousCursor)

		res, err = list(pagination.Query{Limit: 7})
		require.NoError(t, err)
		assert.Len(t, res.Items, 7)
		assert.False(t, res.HasNext, "a page that holds exactly the rest has no next page")

		res, err = list(pagination.Query{Limit: 1000})
		require.NoError(t, err)
		assert.Len(t, res.Items, 7, "a large limit is capped, not refused")
	})

	t.Run("forward and back", func(t *testing.T) {
		pages, results := walk(t, pagination.Query{Limit: 3}, list)
		assert.Equal(t, [][]string{created[:3], created[3:6], created[6:]}, pages)
		assert.Equal(t, created, flatten(pages), "every row exactly once")
		for _, r := range results {
			assert.Equal(t, 7, r.TotalCount)
		}

		// Back from the last page.
		q := pagination.Query{Limit: 3, PreviousCursor: results[2].PreviousCursor}
		back, err := list(q)
		require.NoError(t, err)
		assert.Equal(t, pages[1], idsOf(back.Items))
		assert.True(t, back.HasPrevious)
		assert.True(t, back.HasNext)
		require.NotNil(t, back.NextCursor)

		q.PreviousCursor = back.PreviousCursor
		first, err := list(q)
		require.NoError(t, err)
		assert.Equal(t, pages[0], idsOf(first.Items))
		assert.False(t, first.HasPrevious)
		assert.Nil(t, first.PreviousCursor)
		assert.True(t, first.HasNext)

		// And forward again from a page reached going back.
		again, err := list(pagination.Query{Limit: 3, NextCursor: back.NextCursor})
		require.NoError(t, err)
		assert.Equal(t, pages[2], idsOf(again.Items))
	})

	t.Run("descending", func(t *testing.T) {
		pages, _ := walk(t, pagination.Query{Limit: 3, SortOrder: pagination.SortDescending}, list)
		assert.Equal(t, reversed(created), flatten(pages))
		assert.Len(t, pages, 3)
	})

	for _, tt := range []struct {
		sortBy string
		key    map[string]string
	}{
		{"slug", slugOf},
		{"display_name", nameOf},
	} {
		t.Run("by "+tt.sortBy, func(t *testing.T) {
			want := sortedBy(created, tt.key)
			require.NotEqual(t, created, want, "the sort differs from creation order")

			pages, results := walk(t, pagination.Query{Limit: 2, SortBy: tt.sortBy}, list)
			assert.Equal(t, want, flatten(pages))
			assert.Len(t, pages, 4)

			back, err := list(pagination.Query{Limit: 2, SortBy: tt.sortBy, PreviousCursor: results[3].PreviousCursor})
			require.NoError(t, err)
			assert.Equal(t, pages[2], idsOf(back.Items))

			pages, _ = walk(t, pagination.Query{Limit: 2, SortBy: tt.sortBy, SortOrder: pagination.SortDescending}, list)
			assert.Equal(t, reversed(want), flatten(pages))
		})
	}

	t.Run("invalid queries", func(t *testing.T) {
		first, err := list(pagination.Query{Limit: 2})
		require.NoError(t, err)
		bySlug, err := list(pagination.Query{Limit: 2, SortBy: "slug"})
		require.NoError(t, err)
		badTime := base64.RawURLEncoding.EncodeToString([]byte(`{"s":"created_at","o":"asc","v":"yesterday","id":"env_x"}`))
		notJSON := base64.RawURLEncoding.EncodeToString([]byte(`not json`))

		for name, q := range map[string]pagination.Query{
			"unknown sort field":       {SortBy: "tier"},
			"SQL as a sort field":      {SortBy: "created_at; DROP TABLE delivery.environments"},
			"bad sort order":           {SortOrder: "up"},
			"both cursors":             {NextCursor: first.NextCursor, PreviousCursor: first.NextCursor},
			"cursor from another sort": {NextCursor: bySlug.NextCursor},
			"cursor from another order": {
				SortOrder: pagination.SortDescending, NextCursor: first.NextCursor,
			},
			"slug cursor on created_at": {SortBy: "created_at", NextCursor: bySlug.NextCursor},
			"garbage cursor":            {NextCursor: new("!!!not-base64!!!")},
			"cursor that is not JSON":   {PreviousCursor: &notJSON},
			"cursor with a bad value":   {NextCursor: &badTime},
		} {
			t.Run(name, func(t *testing.T) {
				_, err := list(q)
				assert.ErrorIs(t, err, delivery.ErrInvalid)
			})
		}
		n, err := f.envs.CountActiveEnvironments(f.ctx, orgA, wsA)
		require.NoError(t, err)
		assert.Equal(t, 7, n)
	})
}

func TestPaginationBreaksTiesByID(t *testing.T) {
	f := newFixture(t)
	var ids []string
	for _, slug := range []string{"c", "a", "b", "e", "d"} {
		// The clock is fixed, so every row has the same created_at.
		ids = append(ids, f.createEnv(t, orgA, wsA, slug).ID())
	}
	slices.Sort(ids)
	list := func(q pagination.Query) (*pagination.Result[*delivery.Environment], error) {
		return f.envs.ListEnvironments(f.ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgA, WorkspaceID: wsA, Page: q})
	}

	pages, results := walk(t, pagination.Query{Limit: 2}, list)
	assert.Equal(t, ids, flatten(pages))

	back, err := list(pagination.Query{Limit: 2, PreviousCursor: results[len(results)-1].PreviousCursor})
	require.NoError(t, err)
	assert.Equal(t, pages[len(pages)-2], idsOf(back.Items))

	pages, _ = walk(t, pagination.Query{Limit: 2, SortOrder: pagination.SortDescending}, list)
	assert.Equal(t, reversed(ids), flatten(pages))

}

func TestPaginationOfOtherLists(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	env := f.createEnv(t, orgA, wsA, "env")
	var lifecycles, targets, registries, apps []string
	for i, slug := range []string{"c", "a", "d", "b"} {
		at(t, time.Duration(i)*time.Second)
		lifecycles = append(lifecycles, f.createLifecycle(t, orgA, wsA, slug).ID())
		targets = append(targets, f.createTarget(t, orgA, env.ID(), slug).ID())
		registries = append(registries, f.createRegistry(t, orgA, wsA, slug).ID())
		apps = append(apps, f.createApp(t, orgA, wsA, projA, slug).ID())
	}
	// A lifecycle with phases, so listed pages carry them.
	at(t, time.Minute)
	withPhases := f.createLifecycle(t, orgA, wsA, "e", phase("P", env.ID()))
	lifecycles = append(lifecycles, withPhases.ID())

	t.Run("lifecycles", func(t *testing.T) {
		list := func(q pagination.Query) (*pagination.Result[*delivery.Lifecycle], error) {
			return f.lifecycles.ListLifecycles(ctx, delivery.ListLifecyclesQuery{OrganizationID: orgA, WorkspaceID: wsA, Page: q})
		}
		pages, results := walk(t, pagination.Query{Limit: 2}, list)
		assert.Equal(t, lifecycles, flatten(pages))
		last := results[len(results)-1]
		assert.Equal(t, withPhases.Snapshot(), last.Items[len(last.Items)-1].Snapshot())

		back, err := list(pagination.Query{Limit: 2, PreviousCursor: last.PreviousCursor})
		require.NoError(t, err)
		assert.Equal(t, pages[1], idsOf(back.Items))

		pages, _ = walk(t, pagination.Query{Limit: 2, SortBy: "slug", SortOrder: pagination.SortDescending}, list)
		assert.Equal(t, []string{withPhases.ID(), lifecycles[2], lifecycles[0], lifecycles[3], lifecycles[1]}, flatten(pages))

		_, err = list(pagination.Query{SortBy: "position"})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})

	t.Run("targets", func(t *testing.T) {
		list := func(q pagination.Query) (*pagination.Result[*delivery.Target], error) {
			return f.targets.ListTargets(ctx, delivery.ListTargetsQuery{OrganizationID: orgA, EnvironmentID: env.ID(), Page: q})
		}
		pages, _ := walk(t, pagination.Query{Limit: 3}, list)
		assert.Equal(t, targets, flatten(pages))
		pages, _ = walk(t, pagination.Query{Limit: 1, SortBy: "slug"}, list)
		assert.Equal(t, []string{targets[1], targets[3], targets[0], targets[2]}, flatten(pages))
		_, err := list(pagination.Query{SortBy: "runtime"})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})

	t.Run("registries", func(t *testing.T) {
		list := func(q pagination.Query) (*pagination.Result[*delivery.Registry], error) {
			return f.registries.ListRegistries(ctx, delivery.ListRegistriesQuery{OrganizationID: orgA, WorkspaceID: wsA, Page: q})
		}
		pages, _ := walk(t, pagination.Query{Limit: 3, SortOrder: pagination.SortDescending}, list)
		assert.Equal(t, reversed(registries), flatten(pages))
		_, err := list(pagination.Query{SortBy: "url"})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})

	t.Run("applications", func(t *testing.T) {
		list := func(q pagination.Query) (*pagination.Result[*delivery.Application], error) {
			return f.apps.ListApplications(ctx, delivery.ListApplicationsQuery{OrganizationID: orgA, ProjectID: projA, Page: q})
		}
		pages, _ := walk(t, pagination.Query{Limit: 3}, list)
		assert.Equal(t, apps, flatten(pages))
		pages, _ = walk(t, pagination.Query{Limit: 3, SortBy: "display_name"}, list)
		assert.Equal(t, []string{apps[1], apps[3], apps[0], apps[2]}, flatten(pages))
		_, err := list(pagination.Query{SortBy: "owner_team_id"})
		assert.ErrorIs(t, err, delivery.ErrInvalid)
	})
}

func TestPaginationCursorsMatchFlags(t *testing.T) {

	f := newFixture(t)
	var envs []*delivery.Environment
	for i := range 4 {
		at(t, time.Duration(i)*time.Second)
		envs = append(envs, f.createEnv(t, orgA, wsA, string(rune('a'+i))))
	}
	list := func(q pagination.Query) (*pagination.Result[*delivery.Environment], error) {
		return f.envs.ListEnvironments(f.ctx, delivery.ListEnvironmentsQuery{OrganizationID: orgA, WorkspaceID: wsA, Page: q})
	}
	first, err := list(pagination.Query{Limit: 2})
	require.NoError(t, err)
	second, err := list(pagination.Query{Limit: 2, NextCursor: first.NextCursor})
	require.NoError(t, err)

	// The rows before the second page are archived while the caller holds its cursor.
	for _, e := range envs[:2] {
		e.Archive()
		require.NoError(t, f.envs.UpdateEnvironment(f.ctx, e))
	}
	back, err := list(pagination.Query{Limit: 2, PreviousCursor: second.PreviousCursor})
	require.NoError(t, err)
	assert.Empty(t, back.Items)
	assert.Equal(t, back.HasNext, back.NextCursor != nil, "HasNext without a NextCursor")

	// Likewise forward past the end.
	for _, e := range envs[2:] {
		e.Archive()
		require.NoError(t, f.envs.UpdateEnvironment(f.ctx, e))
	}
	past, err := list(pagination.Query{Limit: 2, NextCursor: first.NextCursor})
	require.NoError(t, err)
	assert.Empty(t, past.Items)
	assert.Equal(t, past.HasPrevious, past.PreviousCursor != nil, "HasPrevious without a PreviousCursor")
}

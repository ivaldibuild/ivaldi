package versioncontrol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

var (
	repoView = vcs.Repository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	}
	renamedView = vcs.Repository{
		ExternalID: "99", FullName: "acme/checkout", DefaultBranch: "trunk", Visibility: "public",
		CloneURL: "https://github.com/acme/checkout.git", WebURL: "https://github.com/acme/checkout",
	}
)

func TestSourceRefresh(t *testing.T) {
	connection := testConnection(t)
	source, err := NewSource(NewSourceProps{
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ConnectionID:   connection.ID(),
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:      "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		RepositoryKey:  connection.RepositoryKey("99"),
		Repository: vcs.Repository{
			ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
		},
	}, nil)
	require.NoError(t, err)
	source.PullEvents()

	renamed := vcs.Repository{
		ExternalID: "99", FullName: "acme/checkout", DefaultBranch: "trunk", Visibility: "public",
		CloneURL: "https://github.com/acme/checkout.git", WebURL: "https://github.com/acme/checkout",
	}
	assert.True(t, refresh(t, source, renamed))
	assert.Equal(t, "acme/checkout", source.FullName())
	assert.Equal(t, "trunk", source.DefaultBranch())
	assert.Equal(t, VisibilityPublic, source.Visibility())
	assert.Equal(t, "https://github.com/acme/checkout", deref(source.WebURL()))
	assert.Len(t, source.PullEvents(), 1)
	assert.True(t, source.Describes(renamed))

	assert.False(t, refresh(t, source, renamed))
	assert.Empty(t, source.PullEvents(), "refreshing unchanged metadata records nothing")

	other := renamed
	other.ExternalID = "100"
	_, err = source.Refresh(other)
	assert.ErrorIs(t, err, ErrInvalid, "another repository cannot refresh the source")

	source.Archive()
	_, err = source.Refresh(renamed)
	assert.ErrorIs(t, err, ErrArchived)
}

// refresh refreshes a Source from a view that must not fail and reports
// whether it changed the Source.
func refresh(t *testing.T, source *Source, view vcs.Repository) bool {
	t.Helper()
	changed, err := source.Refresh(view)
	require.NoError(t, err)
	return changed
}

func TestSourceRefreshIsOrderedWithWebhooks(t *testing.T) {
	webhookAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	t.Run("a refresh older than an applied webhook changes nothing", func(t *testing.T) {
		source := newWebhookSource(t)
		require.True(t, applyView(t, source, renamedView, nil, webhookAt))
		source.PullEvents()

		older := repoView
		older.UpdatedAt = webhookAt.Add(-time.Minute)
		assert.False(t, refresh(t, source, older))
		assert.Equal(t, renamedView.FullName, source.FullName())
		assert.Empty(t, source.PullEvents())
	})

	t.Run("a webhook older than a refresh changes nothing", func(t *testing.T) {
		source := newWebhookSource(t)
		refreshed := renamedView
		refreshed.UpdatedAt = webhookAt
		require.True(t, refresh(t, source, refreshed))
		source.PullEvents()

		assert.False(t, applyView(t, source, repoView, nil, webhookAt.Add(-time.Minute)))
		assert.Equal(t, renamedView.FullName, source.FullName())
	})

	t.Run("a refresh without a provider time is unordered", func(t *testing.T) {
		source := newWebhookSource(t)
		require.True(t, applyView(t, source, renamedView, nil, webhookAt))

		assert.True(t, refresh(t, source, repoView))
		assert.Equal(t, repoView.FullName, source.FullName())
		assert.Equal(t, webhookAt, *source.Snapshot().ProviderUpdatedAt)
	})
}

func TestNewSourceIsOrderedFromItsRepository(t *testing.T) {
	connection := testConnection(t)
	createdFrom := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	view := renamedView
	view.UpdatedAt = createdFrom
	source, err := NewSource(NewSourceProps{
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ConnectionID:   connection.ID(),
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:      "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		RepositoryKey:  connection.RepositoryKey("99"),
		Repository:     view,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, createdFrom, *source.Snapshot().ProviderUpdatedAt)

	assert.False(t, applyView(t, source, repoView, nil, createdFrom.Add(-time.Minute)),
		"a webhook from before the Source was connected changes nothing")
}

func newWebhookSource(t *testing.T) *Source {
	t.Helper()
	connection := testConnection(t)
	source, err := NewSource(NewSourceProps{
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ConnectionID:   connection.ID(),
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:      "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		RepositoryKey:  connection.RepositoryKey("99"),
		Repository:     repoView,
	}, nil)
	require.NoError(t, err)
	source.PullEvents()
	return source
}

// applyView applies a provider view that must not fail and reports whether
// it was applied.
func applyView(t *testing.T, source *Source, view vcs.Repository, archived *bool, at time.Time) bool {
	t.Helper()
	applied, err := source.ApplyProviderView(view, archived, at)
	require.NoError(t, err)
	return applied
}

func TestSourceApplyProviderViewUpdatesMetadataAndArchive(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived, unarchived := true, false

	assert.True(t, applyView(t, source, renamedView, &archived, archivedAt))
	assert.Equal(t, "acme/checkout", source.FullName())
	assert.Equal(t, "trunk", source.DefaultBranch())
	assert.Equal(t, VisibilityPublic, source.Visibility())
	assert.Equal(t, "https://github.com/acme/checkout", deref(source.WebURL()))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1, "one webhook records one event")

	assert.True(t, applyView(t, source, renamedView, &archived, archivedAt))
	assert.Empty(t, source.PullEvents(), "the same view records nothing")

	later := archivedAt.Add(time.Hour)
	assert.True(t, applyView(t, source, renamedView, &archived, later))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt(), "a later view of the same archive keeps its time")
	assert.Empty(t, source.PullEvents())

	unarchivedAt := later.Add(time.Hour)
	assert.True(t, applyView(t, source, renamedView, &unarchived, unarchivedAt))
	assert.Nil(t, source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1)
}

func TestSourceApplyProviderViewRefusesAnOlderView(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived, unarchived := true, false
	require.True(t, applyView(t, source, renamedView, &archived, archivedAt))
	source.PullEvents()

	older := archivedAt.Add(-time.Hour)
	assert.False(t, applyView(t, source, repoView, &unarchived, older), "an older view is refused")
	assert.Equal(t, "acme/checkout", source.FullName(), "an older rename does not undo a newer one")
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt(), "an older view does not undo a newer archive")
	assert.Equal(t, archivedAt, *source.Snapshot().ProviderUpdatedAt)
	assert.Empty(t, source.PullEvents())
}

func TestSourceApplyProviderViewRefusesAnArchiveOlderThanItsUnarchive(t *testing.T) {
	// GitHub archived the repository at 10:00 and unarchived it at 11:00, and
	// the unarchive is handled first. It changes nothing, because the Source
	// was not archived, but it must still refuse the archive.
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived, unarchived := true, false

	assert.True(t, applyView(t, source, repoView, &unarchived, archivedAt.Add(time.Hour)))
	assert.Empty(t, source.PullEvents(), "the unarchive changes nothing")

	assert.False(t, applyView(t, source, repoView, &archived, archivedAt))
	assert.Nil(t, source.UpstreamArchivedAt(), "the repository stays unarchived")
	assert.Empty(t, source.PullEvents())
}

func TestSourceApplyProviderViewAppliesAViewAsOldAsTheNewest(t *testing.T) {
	// Providers report seconds, so two different events can share one.
	source := newWebhookSource(t)
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	require.True(t, applyView(t, source, renamedView, nil, at))
	source.PullEvents()

	assert.True(t, applyView(t, source, repoView, nil, at))
	assert.Equal(t, repoView.FullName, source.FullName())
}

func TestSourceApplyProviderViewWithoutATimeIsUnordered(t *testing.T) {
	source := newWebhookSource(t)
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	require.True(t, applyView(t, source, renamedView, nil, at))

	assert.True(t, applyView(t, source, repoView, nil, time.Time{}), "a view without a time always applies")
	assert.Equal(t, repoView.FullName, source.FullName())
	assert.Equal(t, at, *source.Snapshot().ProviderUpdatedAt, "and does not move the newest time")
}

func TestSourceApplyProviderViewUsesThePayloadArchiveState(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived, unarchived := true, false
	require.True(t, applyView(t, source, repoView, &archived, archivedAt))
	source.PullEvents()

	require.True(t, applyView(t, source, repoView, &unarchived, archivedAt.Add(time.Minute)))
	assert.Nil(t, source.UpstreamArchivedAt(), "a generic change reports the current state")
	assert.Len(t, source.PullEvents(), 1)
}

func TestSourceApplyProviderViewWithoutArchiveStateLeavesItAlone(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived := true
	require.True(t, applyView(t, source, repoView, &archived, archivedAt))
	source.PullEvents()

	require.True(t, applyView(t, source, renamedView, nil, archivedAt.Add(time.Minute)))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1)
}

func TestSourceApplyProviderViewDefaultsAZeroArchiveTime(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	source := newWebhookSource(t)
	archived := true

	require.True(t, applyView(t, source, repoView, &archived, time.Time{}))
	assert.Equal(t, now, *source.UpstreamArchivedAt())
}

func TestSourceApplyProviderViewRefusesADisconnectedSource(t *testing.T) {
	source := newWebhookSource(t)
	source.Archive()
	archived := true

	_, err := source.ApplyProviderView(renamedView, &archived, time.Time{})
	assert.ErrorIs(t, err, ErrArchived)
}

func TestSourceApplyProviderViewRefusesAnotherRepository(t *testing.T) {
	source := newWebhookSource(t)
	other := renamedView
	other.ExternalID = "100"

	_, err := source.ApplyProviderView(other, nil, time.Time{})
	assert.ErrorIs(t, err, ErrInvalid)
}

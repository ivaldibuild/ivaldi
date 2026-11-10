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
	require.NoError(t, source.Refresh(renamed))
	assert.Equal(t, "acme/checkout", source.FullName())
	assert.Equal(t, "trunk", source.DefaultBranch())
	assert.Equal(t, VisibilityPublic, source.Visibility())
	assert.Equal(t, "https://github.com/acme/checkout", deref(source.WebURL()))
	assert.Len(t, source.PullEvents(), 1)
	assert.True(t, source.Describes(renamed))

	require.NoError(t, source.Refresh(renamed))
	assert.Empty(t, source.PullEvents(), "refreshing unchanged metadata records nothing")

	other := renamed
	other.ExternalID = "100"
	assert.ErrorIs(t, source.Refresh(other), ErrInvalid, "another repository cannot refresh the source")

	source.Archive()
	assert.ErrorIs(t, source.Refresh(renamed), ErrArchived)
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

func TestSourceApplyProviderViewUpdatesMetadataAndArchive(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	archived := true
	require.NoError(t, source.ApplyProviderView(renamedView, &archived, archivedAt))
	assert.Equal(t, "acme/checkout", source.FullName())
	assert.Equal(t, "trunk", source.DefaultBranch())
	assert.Equal(t, VisibilityPublic, source.Visibility())
	assert.Equal(t, "https://github.com/acme/checkout", deref(source.WebURL()))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1, "one webhook records one event")

	require.NoError(t, source.ApplyProviderView(renamedView, &archived, archivedAt))
	assert.Empty(t, source.PullEvents(), "the same view records nothing")

	earlier := archivedAt.Add(-time.Hour)
	require.NoError(t, source.ApplyProviderView(renamedView, &archived, earlier))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt(), "an older timestamp does not move it backwards")
	assert.Empty(t, source.PullEvents())

	later := archivedAt.Add(time.Hour)
	require.NoError(t, source.ApplyProviderView(renamedView, &archived, later))
	assert.Equal(t, later, *source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1)

	unarchived := false
	require.NoError(t, source.ApplyProviderView(renamedView, &unarchived, later))
	assert.Nil(t, source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1)

	require.NoError(t, source.ApplyProviderView(renamedView, &unarchived, later))
	assert.Empty(t, source.PullEvents(), "clearing an unarchived Source records nothing")
}

func TestSourceApplyProviderViewUsesThePayloadArchiveState(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived := true
	require.NoError(t, source.ApplyProviderView(repoView, &archived, archivedAt))
	source.PullEvents()

	unarchived := false
	require.NoError(t, source.ApplyProviderView(repoView, &unarchived, time.Time{}))
	assert.Nil(t, source.UpstreamArchivedAt(), "a generic change reports the current state")
	assert.Len(t, source.PullEvents(), 1)
}

func TestSourceApplyProviderViewWithoutArchiveStateLeavesItAlone(t *testing.T) {
	source := newWebhookSource(t)
	archivedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	archived := true
	require.NoError(t, source.ApplyProviderView(repoView, &archived, archivedAt))
	source.PullEvents()

	require.NoError(t, source.ApplyProviderView(renamedView, nil, time.Time{}))
	assert.Equal(t, archivedAt, *source.UpstreamArchivedAt())
	assert.Len(t, source.PullEvents(), 1)
}

func TestSourceApplyProviderViewDefaultsAZeroArchiveTime(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	source := newWebhookSource(t)
	archived := true

	require.NoError(t, source.ApplyProviderView(repoView, &archived, time.Time{}))
	assert.Equal(t, now, *source.UpstreamArchivedAt())
}

func TestSourceApplyProviderViewRefusesADisconnectedSource(t *testing.T) {
	source := newWebhookSource(t)
	source.Archive()
	archived := true

	assert.ErrorIs(t, source.ApplyProviderView(renamedView, &archived, time.Time{}), ErrArchived)
}

func TestSourceApplyProviderViewRefusesAnotherRepository(t *testing.T) {
	source := newWebhookSource(t)
	other := renamedView
	other.ExternalID = "100"

	assert.ErrorIs(t, source.ApplyProviderView(other, nil, time.Time{}), ErrInvalid)
}

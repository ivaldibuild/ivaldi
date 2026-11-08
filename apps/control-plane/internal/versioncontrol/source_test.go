package versioncontrol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
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

package versioncontrol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

func testConnection(t *testing.T) *Connection {
	t.Helper()
	connection, err := NewConnection(NewConnectionProps{
		OrganizationID:    "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Provider:          vcs.ProviderGitHub,
		HostURL:           "https://github.com",
		AuthKind:          AuthKindAppInstallation,
		ExternalAccountID: "42",
		LookupFrom:        vcs.LookupFromControlPlane,
		Status:            StatusActive,
	}, nil)
	require.NoError(t, err)
	connection.PullEvents()
	return connection
}

func TestConnectionStatusTransitions(t *testing.T) {
	connection := testConnection(t)

	require.NoError(t, connection.Suspend())
	assert.Equal(t, StatusSuspended, connection.Status())
	assert.Len(t, connection.PullEvents(), 1)
	require.NoError(t, connection.Suspend())
	assert.Empty(t, connection.PullEvents(), "suspending again records nothing")

	require.NoError(t, connection.Reactivate())
	assert.Equal(t, StatusActive, connection.Status())
	assert.Len(t, connection.PullEvents(), 1)

	require.NoError(t, connection.Revoke())
	assert.Equal(t, StatusRevoked, connection.Status())
	connection.PullEvents()
	assert.ErrorIs(t, connection.Suspend(), ErrInvalid, "a revoked connection cannot be suspended")
	assert.ErrorIs(t, connection.Reactivate(), ErrInvalid, "a revoked connection cannot be reactivated")
	assert.Empty(t, connection.PullEvents())

	connection.Archive()
	assert.ErrorIs(t, connection.Revoke(), ErrArchived)
}

package versioncontrol

import (
	"testing"
	"time"

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

func TestConnectionApplyProviderStatusRefusesAnOlderReport(t *testing.T) {
	// The installation was suspended at 10:00 and unsuspended at 11:00, and
	// the unsuspend is handled first. It changes nothing, because the
	// Connection was active, but it must still refuse the suspension.
	connection := testConnection(t)
	suspendedAt := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	applied, err := connection.ApplyProviderStatus(StatusActive, suspendedAt.Add(time.Hour))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Empty(t, connection.PullEvents())

	applied, err = connection.ApplyProviderStatus(StatusSuspended, suspendedAt)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, StatusActive, connection.Status())
	assert.Empty(t, connection.PullEvents())
}

func TestConnectionApplyProviderStatusAppliesNewerReports(t *testing.T) {
	connection := testConnection(t)
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	applied, err := connection.ApplyProviderStatus(StatusSuspended, at)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, StatusSuspended, connection.Status())
	assert.Len(t, connection.PullEvents(), 1)

	applied, err = connection.ApplyProviderStatus(StatusActive, at.Add(time.Hour))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, StatusActive, connection.Status())
	assert.Equal(t, at.Add(time.Hour), *connection.Snapshot().ProviderUpdatedAt)
}

func TestConnectionApplyProviderStatusRefusedTransitionMovesNothing(t *testing.T) {
	connection := testConnection(t)
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	require.NoError(t, connection.Revoke())

	applied, err := connection.ApplyProviderStatus(StatusActive, at)
	assert.ErrorIs(t, err, ErrInvalid)
	assert.False(t, applied)
	assert.Nil(t, connection.Snapshot().ProviderUpdatedAt)
}

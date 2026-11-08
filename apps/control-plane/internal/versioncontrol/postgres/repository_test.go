package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const (
	orgA = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	orgB = "org_01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

var t0 = time.Date(2026, 11, 8, 9, 30, 15, 123456000, time.UTC)

type fixture struct {
	ctx         context.Context
	db          *gorm.DB
	connections versioncontrol.ConnectionRepository
	sources     versioncontrol.SourceRepository
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, t0)
	return &fixture{
		ctx:         context.Background(),
		db:          db,
		connections: NewConnectionRepository(db),
		sources:     NewSourceRepository(db),
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func newConnection(t *testing.T) *versioncontrol.Connection {
	t.Helper()
	return must(versioncontrol.NewConnection(versioncontrol.NewConnectionProps{
		OrganizationID:    orgA,
		Provider:          vcs.ProviderGitHub,
		HostURL:           "https://github.com",
		AuthKind:          versioncontrol.AuthKindAppInstallation,
		ExternalAccountID: "42",
		LookupFrom:        vcs.LookupFromControlPlane,
		Status:            versioncontrol.StatusActive,
	}, nil))
}

func newSource(t *testing.T, connection *versioncontrol.Connection, externalID string) *versioncontrol.Source {
	t.Helper()
	return must(versioncontrol.NewSource(versioncontrol.NewSourceProps{
		OrganizationID: orgA,
		ConnectionID:   connection.ID(),
		Provider:       connection.Provider(),
		HostURL:        connection.HostURL(),
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:      "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Repository: vcs.Repository{
			ExternalID: externalID, FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
		},
	}, nil))
}

func TestConnectionAndSourceRepositories(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	connection := newConnection(t)
	require.NoError(t, f.connections.CreateConnection(ctx, connection))

	got, err := f.connections.GetConnection(ctx, orgA, connection.ID())
	require.NoError(t, err)
	assert.Equal(t, connection.Snapshot(), got.Snapshot())

	_, err = f.connections.GetConnection(ctx, orgB, connection.ID())
	assert.ErrorIs(t, err, versioncontrol.ErrNotFound)

	source := newSource(t, connection, "99")
	require.NoError(t, f.sources.CreateSource(ctx, source))

	loaded, err := f.sources.GetSource(ctx, orgA, source.ID())
	require.NoError(t, err)
	assert.Equal(t, source.Snapshot(), loaded.Snapshot())

	_, err = f.sources.GetSource(ctx, orgB, source.ID())
	assert.ErrorIs(t, err, versioncontrol.ErrNotFound)

	page, err := f.sources.ListSources(ctx, versioncontrol.ListSourcesQuery{
		OrganizationID: orgA, ProjectID: source.ProjectID(),
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, source.ID(), page.Items[0].ID())
}

// An installation connects to one Organization across the whole platform,
// and an archived Connection frees it.
func TestAnInstallationConnectsToOneOrganization(t *testing.T) {
	f := newFixture(t)
	installation := vcs.Installation{
		Provider: vcs.ProviderGitHub, HostURL: "https://github.com", InstallationID: "42", AccountName: "acme",
	}
	first := must(versioncontrol.NewInstalledConnection(orgA, nil, installation))
	require.NoError(t, f.connections.CreateConnection(f.ctx, first))

	loaded, err := f.connections.GetConnection(f.ctx, orgA, first.ID())
	require.NoError(t, err)
	assert.Equal(t, first.Snapshot(), loaded.Snapshot())

	second := must(versioncontrol.NewInstalledConnection(orgB, nil, installation))
	assert.ErrorIs(t, f.connections.CreateConnection(f.ctx, second), versioncontrol.ErrAccountConnected)

	first.Archive()
	require.NoError(t, f.connections.UpdateConnection(f.ctx, first))
	assert.NoError(t, f.connections.CreateConnection(f.ctx, second))
}

func TestSourceRepositoryKeepsRepositoriesUnique(t *testing.T) {
	f := newFixture(t)
	connection := newConnection(t)
	require.NoError(t, f.connections.CreateConnection(f.ctx, connection))

	first := newSource(t, connection, "99")
	require.NoError(t, f.sources.CreateSource(f.ctx, first))

	second := newSource(t, connection, "99")
	err := f.sources.CreateSource(f.ctx, second)
	assert.ErrorIs(t, err, versioncontrol.ErrAlreadyExists)
}

func TestSourceArchiveAndRestore(t *testing.T) {
	f := newFixture(t)
	connection := newConnection(t)
	require.NoError(t, f.connections.CreateConnection(f.ctx, connection))
	source := newSource(t, connection, "99")
	require.NoError(t, f.sources.CreateSource(f.ctx, source))

	source.Archive()
	require.NoError(t, f.sources.UpdateSource(f.ctx, source))
	got, err := f.sources.GetSource(f.ctx, orgA, source.ID())
	require.NoError(t, err)
	assert.True(t, got.Archived())

	source.Restore()
	require.NoError(t, f.sources.UpdateSource(f.ctx, source))
	got, err = f.sources.GetSource(f.ctx, orgA, source.ID())
	require.NoError(t, err)
	assert.False(t, got.Archived())
}

func TestConnectedRepositoryKeysCountsOnlyActiveSources(t *testing.T) {
	f := newFixture(t)
	connection := newConnection(t)
	require.NoError(t, f.connections.CreateConnection(f.ctx, connection))
	active := newSource(t, connection, "1")
	require.NoError(t, f.sources.CreateSource(f.ctx, active))
	disconnected := newSource(t, connection, "2")
	require.NoError(t, f.sources.CreateSource(f.ctx, disconnected))
	disconnected.Archive()
	require.NoError(t, f.sources.UpdateSource(f.ctx, disconnected))

	keys := []string{connection.RepositoryKey("1"), connection.RepositoryKey("2"), connection.RepositoryKey("3")}
	connected, err := f.sources.ConnectedRepositoryKeys(f.ctx, orgA, keys)
	require.NoError(t, err)
	assert.Equal(t, []string{active.RepositoryKey()}, connected)

	other, err := f.sources.ConnectedRepositoryKeys(f.ctx, orgB, keys)
	require.NoError(t, err)
	assert.Empty(t, other, "another Organization's Sources do not count")
}

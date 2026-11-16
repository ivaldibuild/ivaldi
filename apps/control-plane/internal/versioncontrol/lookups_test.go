package versioncontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type fakeSources struct {
	source *Source
}

func (f fakeSources) CreateSource(context.Context, *Source) error { return nil }
func (f fakeSources) ConnectedRepositoryKeys(context.Context, string, []string) ([]string, error) {
	return nil, nil
}

func (f fakeSources) GetSource(context.Context, string, string) (*Source, error) {
	return f.source, nil
}
func (f fakeSources) GetSourceForUpdate(context.Context, string, string) (*Source, error) {
	return f.source, nil
}
func (f fakeSources) GetSourceByRepositoryKey(context.Context, string, string) (*Source, error) {
	return f.source, nil
}
func (f fakeSources) GetSourceByRepositoryKeyForUpdate(context.Context, string, string) (*Source, error) {
	return f.source, nil
}
func (f fakeSources) ListSources(context.Context, ListSourcesQuery) (*pagination.Result[*Source], error) {
	return nil, nil
}
func (f fakeSources) UpdateSource(context.Context, *Source) error { return nil }

type fakeConnections struct {
	connection *Connection
}

func (f fakeConnections) CreateConnection(context.Context, *Connection) error { return nil }
func (f fakeConnections) GetConnection(context.Context, string, string) (*Connection, error) {
	return f.connection, nil
}
func (f fakeConnections) GetConnectionForUpdate(context.Context, string, string) (*Connection, error) {
	return f.connection, nil
}
func (f fakeConnections) GetConnectionByWebhookRouteKey(context.Context, string) (*Connection, error) {
	return f.connection, nil
}
func (f fakeConnections) GetConnectionByWebhookRouteKeyForUpdate(context.Context, string) (*Connection, error) {
	return f.connection, nil
}
func (f fakeConnections) ListConnections(context.Context, string, bool) ([]*Connection, error) {
	return nil, nil
}
func (f fakeConnections) UpdateConnection(context.Context, *Connection) error { return nil }

type fakeProvider struct {
	gotEndpoint vcs.Endpoint
	comparison  vcs.Comparison
	err         error
}

func (f *fakeProvider) Repositories(context.Context, vcs.Endpoint) ([]vcs.Repository, error) {
	return nil, nil
}

func (f *fakeProvider) Repository(context.Context, vcs.Endpoint, string) (vcs.Repository, error) {
	return vcs.Repository{}, nil
}

func (f *fakeProvider) Compare(_ context.Context, at vcs.Endpoint, _ vcs.Repository, _, _ string) (vcs.Comparison, error) {
	f.gotEndpoint = at
	return f.comparison, f.err
}

func newConnection(t *testing.T, lookup vcs.LookupFrom, pool *string) *Connection {
	t.Helper()
	connection, err := NewConnection(NewConnectionProps{
		OrganizationID:    "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Provider:          vcs.ProviderGitHub,
		HostURL:           "https://github.com",
		AuthKind:          AuthKindAppInstallation,
		ExternalAccountID: "42",
		LookupFrom:        lookup,
		LookupPool:        pool,
		Status:            StatusActive,
	}, nil)
	require.NoError(t, err)
	return connection
}

func newSource(t *testing.T, connection *Connection) *Source {
	t.Helper()
	repositoryKey := vcs.ProviderKey(connection.Provider(), connection.HostURL(), "99")
	source, err := NewSource(NewSourceProps{
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ConnectionID:   connection.ID(),
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:      "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		RepositoryKey:  repositoryKey,
		Repository: vcs.Repository{
			ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
		},
	}, nil)
	require.NoError(t, err)
	return source
}

func TestLookupsResolveAConnectionAndReturnCommits(t *testing.T) {
	connection := newConnection(t, vcs.LookupFromControlPlane, nil)
	source := newSource(t, connection)
	provider := &fakeProvider{comparison: vcs.Comparison{Added: []vcs.Commit{{SHA: "bbb"}}}}
	lookups, err := NewLookups(fakeSources{source: source}, fakeConnections{connection: connection}, provider)
	require.NoError(t, err)

	comparison, err := lookups.Compare(context.Background(), source.OrganizationID(), source.ID(), "aaa", "bbb")
	require.NoError(t, err)
	require.Len(t, comparison.Added, 1)
	assert.Equal(t, "bbb", comparison.Added[0].SHA)
	assert.Equal(t, "42", provider.gotEndpoint.InstallationID)
	assert.Equal(t, vcs.ProviderGitHub, provider.gotEndpoint.Provider)
}

func TestLookupsRefuseADataPlaneConnection(t *testing.T) {
	pool := "pool-a"
	connection := newConnection(t, vcs.LookupFromDataPlane, &pool)
	source := newSource(t, connection)
	lookups, err := NewLookups(fakeSources{source: source}, fakeConnections{connection: connection}, &fakeProvider{})
	require.NoError(t, err)

	_, err = lookups.Compare(context.Background(), source.OrganizationID(), source.ID(), "aaa", "bbb")
	assert.ErrorIs(t, err, vcs.ErrLookupInDataPlane)
}

func TestLookupsRefuseAnArchivedSource(t *testing.T) {
	connection := newConnection(t, vcs.LookupFromControlPlane, nil)
	source := newSource(t, connection)
	source.Archive()
	lookups, err := NewLookups(fakeSources{source: source}, fakeConnections{connection: connection}, &fakeProvider{})
	require.NoError(t, err)

	_, err = lookups.Compare(context.Background(), source.OrganizationID(), source.ID(), "aaa", "bbb")
	assert.ErrorIs(t, err, vcs.ErrSourceArchived)
}

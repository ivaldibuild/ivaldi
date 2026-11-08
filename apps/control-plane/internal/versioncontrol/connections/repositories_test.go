package connections

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// connectedKeys reports the listed keys as connected.
type connectedKeys []string

func (c connectedKeys) ConnectedRepositoryKeys(_ context.Context, _ string, keys []string) ([]string, error) {
	out := []string{}
	for _, k := range keys {
		for _, connected := range c {
			if k == connected {
				out = append(out, k)
			}
		}
	}
	return out, nil
}

// oneConnection serves a single stored Connection.
type oneConnection struct {
	connectionStore
	connection *versioncontrol.Connection
}

func (s *oneConnection) GetConnection(context.Context, string, string) (*versioncontrol.Connection, error) {
	return s.connection, nil
}

type repositoryProvider struct {
	repositories []vcs.Repository
	asked        int
}

func (p *repositoryProvider) Repository(context.Context, vcs.Endpoint, string) (vcs.Repository, error) {
	return vcs.Repository{}, nil
}
func (p *repositoryProvider) Repositories(context.Context, vcs.Endpoint) ([]vcs.Repository, error) {
	p.asked++
	return p.repositories, nil
}
func (p *repositoryProvider) CommitsBetween(context.Context, vcs.Endpoint, vcs.Repository, string, string) ([]vcs.Commit, error) {
	return nil, nil
}

func listService(t *testing.T, lookupFrom vcs.LookupFrom, provider *repositoryProvider) versioncontrol.ConnectionService {
	t.Helper()
	pool := "pool-a"
	connection, err := versioncontrol.NewConnection(versioncontrol.NewConnectionProps{
		OrganizationID: orgID, Provider: vcs.ProviderGitHub, HostURL: "https://github.com",
		AuthKind: versioncontrol.AuthKindAppInstallation, ExternalAccountID: "42",
		LookupFrom: lookupFrom, LookupPool: &pool, Status: versioncontrol.StatusActive,
	}, nil)
	require.NoError(t, err)
	service, err := NewConnectionService(
		WithConnectionRepo(&oneConnection{connection: connection}),
		WithProvider(provider),
		WithConnectedRepositories(connectedKeys{"github:github.com:2"}),
		WithAuthorizer(allow{}),
		WithTransactor(writer{}),
		WithOutboxRecorder(writer{}),
		WithAuditRecorder(auditor{}),
	)
	require.NoError(t, err)
	return service
}

func TestListRepositoriesSortsByNameAndMarksConnectedOnes(t *testing.T) {
	provider := &repositoryProvider{repositories: []vcs.Repository{
		{ExternalID: "1", FullName: "acme/web"}, {ExternalID: "2", FullName: "Acme/API"},
	}}
	repositories, err := listService(t, vcs.LookupFromControlPlane, provider).
		ListRepositories(context.Background(), ada, orgID, "vcn_1")
	require.NoError(t, err)
	require.Len(t, repositories, 2)
	assert.Equal(t, "Acme/API", repositories[0].Repository.FullName)
	assert.True(t, repositories[0].Connected)
	assert.Equal(t, "acme/web", repositories[1].Repository.FullName)
	assert.False(t, repositories[1].Connected)
}

func TestListRepositoriesNeverContactsADataPlaneConnection(t *testing.T) {
	provider := &repositoryProvider{}
	_, err := listService(t, vcs.LookupFromDataPlane, provider).
		ListRepositories(context.Background(), ada, orgID, "vcn_1")
	assert.ErrorIs(t, err, versioncontrol.ErrLookupInDataPlane)
	assert.Zero(t, provider.asked)
}

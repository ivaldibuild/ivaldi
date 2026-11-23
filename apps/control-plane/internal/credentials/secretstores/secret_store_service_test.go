package secretstores

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

const org = "org_acme"

var (
	ctx = context.Background()
	ada = identity.Actor{PrincipalID: "prn_ada"}
)

// world fakes what the service needs: a store per ID, one active Workspace
// with an Environment, another Workspace's Environment, and how many
// variables refer to each store.
type world struct {
	stores       map[string]*credentials.SecretStore
	workspace    *workspace.Workspace
	environments map[string]*delivery.Environment
	inUse        map[string]int
	// lockedStores counts the locking reads, so a test can tell a change
	// holds the store's row while it counts what uses it.
	lockedStores int
	asked        []authorization.Request
	audit        []audit.Event
	svc          credentials.SecretStoreService
	here, far    string
}

func (w *world) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
func (w *world) Record(_ context.Context, e audit.Event) error {
	w.audit = append(w.audit, e)
	return nil
}

type outboxLog struct{}

func (outboxLog) Record(context.Context, ...events.Envelope) error { return nil }

func (w *world) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	w.asked = append(w.asked, req)
	return authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}, nil
}

func (w *world) CreateSecretStore(_ context.Context, s *credentials.SecretStore) error {
	w.stores[s.ID()] = s
	return nil
}
func (w *world) UpdateSecretStore(ctx context.Context, s *credentials.SecretStore) error {
	return w.CreateSecretStore(ctx, s)
}
func (w *world) GetSecretStore(_ context.Context, _, id string) (*credentials.SecretStore, error) {
	if s, ok := w.stores[id]; ok {
		return s, nil
	}
	return nil, credentials.ErrNotFound
}
func (w *world) GetSecretStoreForUpdate(ctx context.Context, organizationID, id string) (*credentials.SecretStore, error) {
	w.lockedStores++
	return w.GetSecretStore(ctx, organizationID, id)
}
func (w *world) GetSecretStores(context.Context, string, []string) ([]*credentials.SecretStore, error) {
	return nil, nil
}
func (w *world) GetSecretStoresForShare(context.Context, string, []string) ([]*credentials.SecretStore, error) {
	return nil, nil
}
func (w *world) ListSecretStores(context.Context, string, string, bool) ([]*credentials.SecretStore, error) {
	return nil, nil
}
func (w *world) CountActiveSecretStores(context.Context, string, string) (int, error) {
	return len(w.stores), nil
}
func (w *world) LookupWorkspace(_ context.Context, _, id string) (*workspace.Workspace, error) {
	if id != w.workspace.ID() {
		return nil, workspace.ErrNotFound
	}
	return w.workspace, nil
}
func (w *world) GetEnvironments(_ context.Context, _ string, ids []string) ([]*delivery.Environment, error) {
	var out []*delivery.Environment
	for _, id := range ids {
		if e, ok := w.environments[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
func (w *world) CountActiveVariablesWithSecretStore(_ context.Context, _, id string) (int, error) {
	return w.inUse[id], nil
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ws, err := workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: "platform", DisplayName: "Platform"}, nil)
	require.NoError(t, err)
	w := &world{stores: map[string]*credentials.SecretStore{}, workspace: ws, environments: map[string]*delivery.Environment{}, inUse: map[string]int{}}
	env := func(workspaceID, slug string) string {
		e, err := delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Tier: delivery.TierProduction,
		}, nil)
		require.NoError(t, err)
		w.environments[e.ID()] = e
		return e.ID()
	}
	w.here, w.far = env(ws.ID(), "production"), env("wsp_01BX5ZZKBKACTAV9WEVGEMMVRZ", "far")
	w.svc, err = NewSecretStoreService(
		WithSecretStoreRepo(w), WithWorkspaces(w), WithEnvironmentLookup(w), WithVariableUsage(w),
		WithAuthorizer(w), WithTransactor(w), WithOutboxRecorder(outboxLog{}), WithAuditRecorder(w),
	)
	require.NoError(t, err)
	return w
}

func (w *world) create(environments ...string) (*credentials.SecretStore, error) {
	return w.svc.CreateSecretStore(ctx, ada, credentials.CreateSecretStoreInput{
		OrganizationID: org, WorkspaceID: w.workspace.ID(), DisplayName: "In-cluster",
		Kind: secretstore.KindKubernetes, EnvironmentIDs: environments,
	})
}

func TestCreateSecretStore(t *testing.T) {
	w := newWorld(t)
	store, err := w.create(w.here)
	require.NoError(t, err)

	require.Len(t, w.asked, 1)
	assert.Equal(t, credentials.PermissionSecretStoreManage, w.asked[0].Permission)
	assert.Equal(t, w.workspace.ID(), w.asked[0].ResourceID, "checked on the workspace")
	require.Len(t, w.audit, 1)
	assert.Equal(t, credentials.EventSecretStoreCreated, w.audit[0].Type)
	assert.Equal(t, store.ID(), w.audit[0].ResourceID)

	_, err = w.create(w.far)
	assert.ErrorIs(t, err, credentials.ErrWorkspaceMismatch, "an environment in another workspace")
	_, err = w.create("env_01BX5ZZKBKACTAV9WEVGEMMVRZ")
	assert.ErrorIs(t, err, credentials.ErrInvalid, "an environment that does not exist")
	assert.Len(t, w.stores, 1)
}

func TestUpdateSecretStoreChecksItsEnvironments(t *testing.T) {
	w := newWorld(t)
	store, err := w.create()
	require.NoError(t, err)

	_, err = w.svc.UpdateSecretStore(ctx, ada, org, store.ID(), credentials.UpdateSecretStoreProps{EnvironmentIDs: &[]string{w.far}})
	assert.ErrorIs(t, err, credentials.ErrWorkspaceMismatch)
	updated, err := w.svc.UpdateSecretStore(ctx, ada, org, store.ID(), credentials.UpdateSecretStoreProps{EnvironmentIDs: &[]string{w.here}})
	require.NoError(t, err)
	assert.True(t, updated.Allows(w.here))
}

func TestFederatedModeNeedsFederationKeys(t *testing.T) {
	w := newWorld(t)
	federated := credentials.CreateSecretStoreInput{
		OrganizationID: org, WorkspaceID: w.workspace.ID(), DisplayName: "AWS",
		Kind: secretstore.KindAWSSecretsManager, Settings: credentials.Settings{AWS: &credentials.AWSSettings{Region: "eu-west-1"}},
		Access: credentials.Access{Mode: credentials.AccessFederated, RoleARN: "arn:aws:iam::1:role/ivaldi"},
	}
	_, err := w.svc.CreateSecretStore(ctx, ada, federated)
	require.ErrorIs(t, err, credentials.ErrInvalid)
	assert.Contains(t, err.Error(), "no federation signing keys")

	federated.Access = credentials.Access{}
	store, err := w.svc.CreateSecretStore(ctx, ada, federated)
	require.NoError(t, err)
	_, err = w.svc.UpdateSecretStore(ctx, ada, org, store.ID(), credentials.UpdateSecretStoreProps{
		Access: &credentials.Access{Mode: credentials.AccessFederated, RoleARN: "arn:aws:iam::1:role/ivaldi"},
	})
	assert.ErrorIs(t, err, credentials.ErrInvalid, "nor moved into it")
}

func TestArchiveSecretStore(t *testing.T) {
	w := newWorld(t)
	store, err := w.create()
	require.NoError(t, err)

	w.inUse[store.ID()] = 2
	_, err = w.svc.ArchiveSecretStore(ctx, ada, org, store.ID())
	require.ErrorIs(t, err, credentials.ErrInUse, "variables still keep values in it")
	assert.Positive(t, w.lockedStores, "the count runs under the store's row, which a save holds FOR SHARE")

	w.inUse[store.ID()] = 0
	for range 2 {
		archived, err := w.svc.ArchiveSecretStore(ctx, ada, org, store.ID())
		require.NoError(t, err)
		assert.True(t, archived.Archived())
	}
	assert.Equal(t, credentials.EventSecretStoreArchived, w.audit[len(w.audit)-1].Type)
	assert.Len(t, w.audit, 2, "archiving twice records once")
}

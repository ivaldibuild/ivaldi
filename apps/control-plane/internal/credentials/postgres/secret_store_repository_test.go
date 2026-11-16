package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

func TestSecretStoreRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, clock.Now())
	ctx := context.Background()
	stores := NewSecretStoreRepository(db)
	const org, ws, env = "org_a", "wsp_a", "env_01BX5ZZKBKACTAV9WEVGEMMVRA"

	newStore := func(name string, kind secretstore.Kind, settings credentials.Settings, environments ...string) *credentials.SecretStore {
		s, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
			OrganizationID: org, WorkspaceID: ws, DisplayName: name, Kind: kind, Settings: settings, EnvironmentIDs: environments,
		}, nil)
		require.NoError(t, err)
		return s
	}
	vault := newStore("Vault", secretstore.KindVault, credentials.Settings{Vault: &credentials.VaultSettings{
		Address: "https://vault.example.com", Mount: "kv", AuthRole: "ivaldi", Namespace: "payments",
	}}, env)
	cluster := newStore("In-cluster", secretstore.KindKubernetes, credentials.Settings{})
	require.NoError(t, stores.CreateSecretStore(ctx, vault))
	require.NoError(t, stores.CreateSecretStore(ctx, cluster))

	got, err := stores.GetSecretStore(ctx, org, vault.ID())
	require.NoError(t, err)
	assert.Equal(t, vault.Snapshot(), got.Snapshot(), "round trip")
	_, err = stores.GetSecretStore(ctx, "org_b", vault.ID())
	assert.ErrorIs(t, err, credentials.ErrNotFound, "another organization's store")

	assert.ErrorIs(t, stores.CreateSecretStore(ctx, newStore("Vault", secretstore.KindKubernetes, credentials.Settings{})), credentials.ErrSlugTaken)

	listed, err := stores.ListSecretStores(ctx, org, ws, false)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, "In-cluster", listed[0].DisplayName(), "by name")

	require.NoError(t, vault.Update(credentials.UpdateSecretStoreProps{EnvironmentIDs: &[]string{}}))
	vault.Archive()
	require.NoError(t, stores.UpdateSecretStore(ctx, vault))
	n, err := stores.CountActiveSecretStores(ctx, org, ws)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	both, err := stores.GetSecretStores(ctx, org, []string{vault.ID(), cluster.ID(), "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ"})
	require.NoError(t, err)
	assert.Len(t, both, 2, "archived ones included, unknown ones left out")
	assert.NoError(t, stores.CreateSecretStore(ctx, newStore("Vault", secretstore.KindKubernetes, credentials.Settings{})), "an archived store frees its slug")

	t.Run("reads for share and for update hold the rows", func(t *testing.T) {
		tx := database.NewTransactor(db)
		other := func() error {
			return db.Exec("SELECT 1 FROM credentials.secret_stores WHERE id = ? FOR UPDATE NOWAIT", cluster.ID()).Error
		}
		require.NoError(t, tx.WithinTx(ctx, func(ctx context.Context) error {
			got, err := stores.GetSecretStoresForShare(ctx, org, []string{cluster.ID()})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Error(t, other(), "an archive cannot take a store a save holds")
			require.NoError(t, db.Exec("SELECT 1 FROM credentials.secret_stores WHERE id = ? FOR SHARE NOWAIT", cluster.ID()).Error,
				"another save still can")
			return nil
		}))
		require.NoError(t, tx.WithinTx(ctx, func(ctx context.Context) error {
			_, err := stores.GetSecretStoreForUpdate(ctx, org, cluster.ID())
			require.NoError(t, err)
			assert.Error(t, other(), "the archive holds the row")
			return nil
		}))
		assert.NoError(t, other(), "released at commit")
	})
}

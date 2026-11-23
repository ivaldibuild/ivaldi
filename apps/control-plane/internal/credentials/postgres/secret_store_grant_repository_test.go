package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

func TestSecretStoreGrantRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, clock.Now())
	ctx := context.Background()
	const org = "org_a"

	store, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
		OrganizationID: org, WorkspaceID: "wsp_a", DisplayName: "AWS", Kind: secretstore.KindAWSSecretsManager,
		Settings: credentials.Settings{AWS: &credentials.AWSSettings{Region: "eu-west-1"}},
		Access:   credentials.Access{Mode: credentials.AccessFederated, RoleARN: "arn:aws:iam::1:role/ivaldi"},
	}, nil)
	require.NoError(t, err)
	require.NoError(t, NewSecretStoreRepository(db).CreateSecretStore(ctx, store))

	grant := func(deployment string) *credentials.SecretStoreGrant {
		g, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: org, StoreID: store.ID(), DeploymentID: deployment, Mode: credentials.AccessFederated,
			TokenID: "01JWT", ExpiresAt: clock.Now().Add(5 * time.Minute),
		}, nil)
		require.NoError(t, err)
		return g
	}
	grants := NewSecretStoreGrantRepository(db)
	first, other := grant("dpl_a"), grant("dpl_b")
	require.NoError(t, grants.CreateSecretStoreGrants(ctx, []*credentials.SecretStoreGrant{first, other}))

	got, err := grants.ListDeploymentGrants(ctx, org, "dpl_a")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, first.Snapshot(), got[0].Snapshot(), "round trip")
	none, err := grants.ListDeploymentGrants(ctx, "org_b", "dpl_a")
	require.NoError(t, err)
	assert.Empty(t, none, "another organization's grants")
}

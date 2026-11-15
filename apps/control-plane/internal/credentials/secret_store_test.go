package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

const (
	testEnvA = "env_01BX5ZZKBKACTAV9WEVGEMMVRA"
	testEnvB = "env_01BX5ZZKBKACTAV9WEVGEMMVRB"
)

func storeProps(kind secretstore.Kind, settings Settings, environments ...string) NewSecretStoreProps {
	return NewSecretStoreProps{
		OrganizationID: "org_a", WorkspaceID: "wsp_a", DisplayName: "Production secrets",
		Kind: kind, Settings: settings, EnvironmentIDs: environments,
	}
}

// A store holds only the settings its kind needs to be found, never a credential.
func TestNewSecretStore(t *testing.T) {
	vault := Settings{Vault: &VaultSettings{Address: "https://vault.example.com", Mount: "kv", AuthRole: "ivaldi"}}
	doppler := Settings{Doppler: &DopplerSettings{TokenSecretName: "doppler", TokenSecretKey: "token"}}

	valid := []NewSecretStoreProps{
		storeProps(secretstore.KindKubernetes, Settings{}),
		storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, testEnvA),
		storeProps(secretstore.KindVault, vault),
		storeProps(secretstore.KindDoppler, doppler, testEnvA),
	}
	for _, props := range valid {
		s, err := NewSecretStore(props, nil)
		require.NoError(t, err, props.Kind)
		assert.Equal(t, "production-secrets", s.Slug(), "derived from the name")
		assert.Equal(t, []any{SecretStoreCreated{s.Snapshot()}}, []any{s.PullEvents()[0]})
	}

	refused := []struct {
		name   string
		props  NewSecretStoreProps
		reason string
	}{
		{"an unknown kind", storeProps("gcp", Settings{}), "must be kubernetes"},
		{"settings of another kind", storeProps(secretstore.KindKubernetes, vault), "takes no vault settings"},
		{"aws without a region", storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{}}), "the AWS region"},
		{"vault without settings", storeProps(secretstore.KindVault, Settings{}), "needs its address"},
		{"a vault address that is not a URL", storeProps(secretstore.KindVault, Settings{Vault: &VaultSettings{Address: "vault", Mount: "kv", AuthRole: "r"}}), "http or https URL"},
		{"doppler for every environment", storeProps(secretstore.KindDoppler, doppler), "names the environments"},
		{"something that is not an environment", storeProps(secretstore.KindKubernetes, Settings{}, "tgt_01BX5ZZKBKACTAV9WEVGEMMVRA"), "not an environment ID"},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSecretStore(tt.props, nil)
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tt.reason)
		})
	}
}

func TestSecretStoreAllowsItsEnvironments(t *testing.T) {
	everywhere, err := NewSecretStore(storeProps(secretstore.KindKubernetes, Settings{}), nil)
	require.NoError(t, err)
	assert.True(t, everywhere.Allows(testEnvA), "a store naming no environment serves its whole workspace")

	one, err := NewSecretStore(storeProps(secretstore.KindKubernetes, Settings{}, testEnvB, testEnvB), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{testEnvB}, one.EnvironmentIDs())
	assert.False(t, one.Allows(testEnvA))
	assert.True(t, one.Allows(testEnvB))
}

func TestSecretStoreUpdate(t *testing.T) {
	s, err := NewSecretStore(storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, testEnvA), nil)
	require.NoError(t, err)
	s.PullEvents()

	require.NoError(t, s.Update(UpdateSecretStoreProps{DisplayName: new("Production secrets")}))
	assert.Empty(t, s.PullEvents(), "nothing changed")

	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{Settings: &Settings{}}), ErrInvalid, "its kind still needs its settings")
	assert.Equal(t, "eu-west-1", s.Settings().AWS.Region, "a refused update changes nothing")

	require.NoError(t, s.Update(UpdateSecretStoreProps{
		Settings: &Settings{AWS: &AWSSettings{Region: "us-east-1", RoleARN: "arn:aws:iam::1:role/ivaldi"}}, EnvironmentIDs: &[]string{},
	}))
	assert.Equal(t, "us-east-1", s.Settings().AWS.Region)
	assert.Empty(t, s.EnvironmentIDs())
	assert.Len(t, s.PullEvents(), 1)

	s.Archive()
	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{DisplayName: new("x")}), ErrArchived)
}

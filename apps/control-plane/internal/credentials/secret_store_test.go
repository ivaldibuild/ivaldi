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

func storeProps(kind secretstore.Kind, settings Settings, access Access, environments ...string) NewSecretStoreProps {
	return NewSecretStoreProps{
		OrganizationID: "org_a", WorkspaceID: "wsp_a", DisplayName: "Production secrets",
		Kind: kind, Settings: settings, Access: access, EnvironmentIDs: environments,
	}
}

// A store holds where it is and how it is opened, never a credential.
func TestNewSecretStore(t *testing.T) {
	vault := Settings{Vault: &VaultSettings{Address: "https://vault.example.com", Mount: "kv"}}
	vaultAccess := Access{Role: "ivaldi"}
	dopplerAccess := Access{Credential: "doppler-production"}

	valid := []NewSecretStoreProps{
		storeProps(secretstore.KindKubernetes, Settings{}, Access{}),
		storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{RoleARN: "arn:aws:iam::1:role/ivaldi"}, testEnvA),
		storeProps(secretstore.KindVault, vault, vaultAccess),
		storeProps(secretstore.KindDoppler, Settings{}, dopplerAccess, testEnvA),
		storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, dopplerAccess, testEnvA),
	}
	for _, props := range valid {
		s, err := NewSecretStore(props, nil)
		require.NoError(t, err, props.Kind)
		assert.Equal(t, "production-secrets", s.Slug(), "derived from the name")
		assert.Equal(t, AccessAgent, s.Access().Mode, "agent mode when none is given")
		assert.Equal(t, []any{SecretStoreCreated{s.Snapshot()}}, []any{s.PullEvents()[0]})
	}

	refused := []struct {
		name   string
		props  NewSecretStoreProps
		reason string
	}{
		{"an unknown kind", storeProps("gcp", Settings{}, Access{}), "must be kubernetes"},
		{"settings of another kind", storeProps(secretstore.KindKubernetes, vault, Access{}), "takes no vault settings"},
		{"aws without a region", storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{}}, Access{}), "the AWS region"},
		{"vault without settings", storeProps(secretstore.KindVault, Settings{}, vaultAccess), "needs its address"},
		{"a vault address that is not a URL", storeProps(secretstore.KindVault, Settings{Vault: &VaultSettings{Address: "vault", Mount: "kv"}}, vaultAccess), "http or https URL"},
		{"vault without its role", storeProps(secretstore.KindVault, vault, Access{}), "needs the auth role"},
		{"doppler with half a config", storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments"}}, dopplerAccess, testEnvA), "the Doppler config"},
		{"doppler without a credential", storeProps(secretstore.KindDoppler, Settings{}, Access{}, testEnvA), "needs the name of the credential"},
		{"a credential name that is not a DNS name", storeProps(secretstore.KindDoppler, Settings{}, Access{Credential: "Doppler_Prod"}, testEnvA), "lowercase letters"},
		{"doppler for every environment", storeProps(secretstore.KindDoppler, Settings{}, dopplerAccess), "names the environments"},
		{"another mode", storeProps(secretstore.KindKubernetes, Settings{}, Access{Mode: "brokered"}), "must be agent"},
		{"access another kind takes", storeProps(secretstore.KindKubernetes, Settings{}, Access{Credential: "doppler"}), "takes no credential"},
		{"something that is not an environment", storeProps(secretstore.KindKubernetes, Settings{}, Access{}, "tgt_01BX5ZZKBKACTAV9WEVGEMMVRA"), "not an environment ID"},
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
	everywhere, err := NewSecretStore(storeProps(secretstore.KindKubernetes, Settings{}, Access{}), nil)
	require.NoError(t, err)
	assert.True(t, everywhere.Allows(testEnvA), "a store naming no environment serves its whole workspace")

	one, err := NewSecretStore(storeProps(secretstore.KindKubernetes, Settings{}, Access{}, testEnvB, testEnvB), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{testEnvB}, one.EnvironmentIDs())
	assert.False(t, one.Allows(testEnvA))
	assert.True(t, one.Allows(testEnvB))
}

func TestSecretStoreUpdate(t *testing.T) {
	s, err := NewSecretStore(storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{}, testEnvA), nil)
	require.NoError(t, err)
	s.PullEvents()

	require.NoError(t, s.Update(UpdateSecretStoreProps{DisplayName: new("Production secrets")}))
	assert.Empty(t, s.PullEvents(), "nothing changed")

	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{Settings: &Settings{}}), ErrInvalid, "its kind still needs its settings")
	assert.Equal(t, "eu-west-1", s.Settings().AWS.Region, "a refused update changes nothing")

	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{
		Settings: &Settings{AWS: &AWSSettings{Region: "us-east-1"}}, Access: &Access{Role: "ivaldi"},
	}), ErrInvalid, "an aws store takes no vault role")
	assert.Equal(t, "eu-west-1", s.Settings().AWS.Region, "nothing changes if any field is invalid")

	require.NoError(t, s.Update(UpdateSecretStoreProps{
		Settings: &Settings{AWS: &AWSSettings{Region: "us-east-1"}}, EnvironmentIDs: &[]string{},
	}))
	assert.Equal(t, "us-east-1", s.Settings().AWS.Region)
	assert.Empty(t, s.EnvironmentIDs())
	assert.Len(t, s.PullEvents(), 1)

	require.NoError(t, s.Update(UpdateSecretStoreProps{Access: &Access{RoleARN: "arn:aws:iam::1:role/ivaldi"}}))
	assert.Equal(t, Access{Mode: AccessAgent, RoleARN: "arn:aws:iam::1:role/ivaldi"}, s.Access())
	assert.Len(t, s.PullEvents(), 1, "a change of access is a change")

	s.Archive()
	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{DisplayName: new("x")}), ErrArchived)
}

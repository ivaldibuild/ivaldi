package credentials

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
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
	infisical := Settings{Infisical: &InfisicalSettings{ProjectID: "prj_1", Environment: "prod"}}

	valid := []NewSecretStoreProps{
		storeProps(secretstore.KindKubernetes, Settings{}, Access{}),
		storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{RoleARN: "arn:aws:iam::1:role/ivaldi"}, testEnvA),
		storeProps(secretstore.KindVault, vault, vaultAccess),
		storeProps(secretstore.KindDoppler, Settings{}, dopplerAccess, testEnvA),
		storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, dopplerAccess, testEnvA),
		storeProps(secretstore.KindInfisical, infisical, Access{Method: AuthKubernetes, IdentityID: "idt_1"}),
		storeProps(secretstore.KindInfisical, infisical, Access{Method: AuthUniversal, Credential: "infisical-production"}),
	}
	federated := []NewSecretStoreProps{
		storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{Mode: AccessFederated, RoleARN: "arn:aws:iam::1:role/ivaldi"}),
		storeProps(secretstore.KindVault, vault, Access{Mode: AccessFederated, Role: "ivaldi-production"}),
		storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, Access{Mode: AccessFederated, IdentityID: "said_1"}, testEnvA),
		storeProps(secretstore.KindInfisical, infisical, Access{Mode: AccessFederated, Method: AuthOIDC, IdentityID: "idt_1"}),
		storeProps(secretstore.KindInfisical, infisical, Access{Mode: AccessFederated, Method: AuthJWT, IdentityID: "idt_1"}),
	}
	for _, props := range federated {
		s, err := NewSecretStore(props, nil)
		require.NoError(t, err, props.Kind)
		assert.Equal(t, AccessFederated, s.Access().Mode)
	}
	brokered := []NewSecretStoreProps{
		storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{Mode: AccessBrokered, RoleARN: "arn:aws:iam::1:role/ivaldi"}),
		storeProps(secretstore.KindVault, vault, Access{Mode: AccessBrokered, TokenRole: "payments-production"}),
		storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, Access{Mode: AccessBrokered}, testEnvA),
		storeProps(secretstore.KindInfisical, infisical, Access{Mode: AccessBrokered}),
	}
	for _, props := range brokered {
		s, err := NewSecretStore(props, nil)
		require.NoError(t, err, props.Kind)
		assert.Equal(t, props.Kind != secretstore.KindAWSSecretsManager, s.NeedsCredential(), props.Kind)
		assert.False(t, s.CredentialSet())
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
		{"an unknown mode", storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{Mode: "sealed"}), "must be agent, federated, or brokered"},
		{"brokered kubernetes", storeProps(secretstore.KindKubernetes, Settings{}, Access{Mode: AccessBrokered}), "always in agent mode"},
		{"brokered vault without a token role", storeProps(secretstore.KindVault, vault, Access{Mode: AccessBrokered}), "needs the token_role"},
		{"brokered doppler without a config", storeProps(secretstore.KindDoppler, Settings{}, Access{Mode: AccessBrokered}, testEnvA), "needs the Doppler project and config"},
		{"brokered infisical with a method", storeProps(secretstore.KindInfisical, infisical, Access{Mode: AccessBrokered, Method: AuthUniversal}), "takes no method"},
		{"kubernetes federated", storeProps(secretstore.KindKubernetes, Settings{}, Access{Mode: AccessFederated}), "always in agent mode"},
		{"federated aws without a role", storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}}, Access{Mode: AccessFederated}), "needs the role_arn"},
		{"federated doppler without an identity", storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, Access{Mode: AccessFederated}, testEnvA), "needs the identity_id"},
		{"federated doppler without a config", storeProps(secretstore.KindDoppler, Settings{}, Access{Mode: AccessFederated, IdentityID: "said_1"}, testEnvA), "needs the Doppler project and config"},
		{"a credential in federated mode", storeProps(secretstore.KindDoppler, Settings{Doppler: &DopplerSettings{Project: "payments", Config: "prd"}}, Access{Mode: AccessFederated, IdentityID: "said_1", Credential: "doppler"}, testEnvA), "takes no credential"},
		{"infisical without a project", storeProps(secretstore.KindInfisical, Settings{Infisical: &InfisicalSettings{Environment: "prod"}}, Access{Method: AuthKubernetes, IdentityID: "idt_1"}), "the Infisical project ID"},
		{"an infisical folder that is not absolute", storeProps(secretstore.KindInfisical, Settings{Infisical: &InfisicalSettings{ProjectID: "prj_1", Environment: "prod", SecretPath: "app"}}, Access{Method: AuthKubernetes, IdentityID: "idt_1"}), "must start with /"},
		{"infisical without a method", storeProps(secretstore.KindInfisical, infisical, Access{IdentityID: "idt_1"}), "logs in with method kubernetes or universal_auth"},
		{"an infisical method of the other mode", storeProps(secretstore.KindInfisical, infisical, Access{Mode: AccessFederated, Method: AuthKubernetes, IdentityID: "idt_1"}), "logs in with method jwt or oidc"},
		{"universal auth without a credential", storeProps(secretstore.KindInfisical, infisical, Access{Method: AuthUniversal}), "needs the name of the credential that holds its Universal Auth"},
		{"universal auth with an identity", storeProps(secretstore.KindInfisical, infisical, Access{Method: AuthUniversal, Credential: "infisical", IdentityID: "idt_1"}), "takes no identity_id"},
		{"a method for a kind with one way to log in", storeProps(secretstore.KindVault, vault, Access{Role: "ivaldi", Method: AuthKubernetes}), "takes no method"},
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

	require.NoError(t, s.Update(UpdateSecretStoreProps{Access: &Access{Mode: AccessFederated, RoleARN: "arn:aws:iam::1:role/ivaldi"}}))
	assert.Equal(t, AccessFederated, s.Access().Mode, "a store can move to federated mode")

	s.Archive()
	require.ErrorIs(t, s.Update(UpdateSecretStoreProps{DisplayName: new("x")}), ErrArchived)
}

// An infisical store is on Infisical Cloud, at its project's root folder,
// unless it says otherwise, so the agent is always sent both.
func TestInfisicalSettingsDefaults(t *testing.T) {
	s, err := NewSecretStore(storeProps(secretstore.KindInfisical,
		Settings{Infisical: &InfisicalSettings{ProjectID: "prj_1", Environment: "prod"}},
		Access{Method: AuthKubernetes, IdentityID: "idt_1"}), nil)
	require.NoError(t, err)
	assert.Equal(t, InfisicalSettings{SiteURL: secretstore.DefaultInfisicalSite, ProjectID: "prj_1", Environment: "prod", SecretPath: "/"},
		*s.Settings().Infisical)

	s, err = NewSecretStore(storeProps(secretstore.KindInfisical,
		Settings{Infisical: &InfisicalSettings{SiteURL: "https://infisical.example.com/", ProjectID: "prj_1", Environment: "prod", SecretPath: "/app/"}},
		Access{Method: AuthKubernetes, IdentityID: "idt_1"}), nil)
	require.NoError(t, err)
	assert.Equal(t, "https://infisical.example.com", s.Settings().Infisical.SiteURL)
	assert.Equal(t, "/app", s.Settings().Infisical.SecretPath)
}

// A brokered store records when and by whom its minting credential was set,
// never the credential, and drops it when the store's mode or where it is
// changes, or it is archived.
func TestSecretStoreMintingCredential(t *testing.T) {
	clock.Fix(t, time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC))
	vault := Settings{Vault: &VaultSettings{Address: "https://vault.example.com", Mount: "kv"}}
	newStore := func(t *testing.T) *SecretStore {
		s, err := NewSecretStore(storeProps(secretstore.KindVault, vault, Access{Mode: AccessBrokered, TokenRole: "payments"}), nil)
		require.NoError(t, err)
		s.PullEvents()
		return s
	}
	credential := MintingCredential{RoleID: "role", SecretID: "secret"}

	s := newStore(t)
	_, err := s.SetCredential("prn_ada", MintingCredential{RoleID: "role"})
	assert.ErrorContains(t, err, "needs secret_id")
	_, err = s.SetCredential("prn_ada", MintingCredential{RoleID: "role", SecretID: "secret", Token: "dp.st"})
	assert.ErrorContains(t, err, "takes no token")
	kept, err := s.SetCredential("prn_ada", credential)
	require.NoError(t, err)
	assert.Equal(t, credential, kept)
	snap := s.Snapshot()
	assert.Equal(t, "prn_ada", *snap.CredentialSetBy)
	assert.NotNil(t, snap.CredentialSetAt)
	recorded, err := json.Marshal(s.PullEvents())
	require.NoError(t, err)
	assert.NotContains(t, string(recorded), "secret\"", "never the credential")

	cases := []struct {
		name   string
		change func(s *SecretStore) error
		drops  bool
	}{
		{"renamed", func(s *SecretStore) error { return s.Update(UpdateSecretStoreProps{DisplayName: new("Vault")}) }, false},
		{"another token role", func(s *SecretStore) error {
			return s.Update(UpdateSecretStoreProps{Access: &Access{Mode: AccessBrokered, TokenRole: "other"}})
		}, false},
		{"another address", func(s *SecretStore) error {
			return s.Update(UpdateSecretStoreProps{Settings: &Settings{Vault: &VaultSettings{Address: "https://elsewhere.example.com", Mount: "kv"}}})
		}, true},
		{"another mode", func(s *SecretStore) error {
			return s.Update(UpdateSecretStoreProps{Access: &Access{Mode: AccessAgent, Role: "ivaldi"}})
		}, true},
		{"archived", func(s *SecretStore) error { s.Archive(); return nil }, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore(t)
			_, err := s.SetCredential("prn_ada", credential)
			require.NoError(t, err)
			require.NoError(t, tt.change(s))
			assert.Equal(t, !tt.drops, s.CredentialSet())
		})
	}

	aws, err := NewSecretStore(storeProps(secretstore.KindAWSSecretsManager, Settings{AWS: &AWSSettings{Region: "eu-west-1"}},
		Access{Mode: AccessBrokered, RoleARN: "arn:aws:iam::1:role/ivaldi"}), nil)
	require.NoError(t, err)
	_, err = aws.SetCredential("prn_ada", MintingCredential{Token: "x"})
	assert.ErrorContains(t, err, "takes no credential")
	agent, err := NewSecretStore(storeProps(secretstore.KindVault, vault, Access{Role: "ivaldi"}), nil)
	require.NoError(t, err)
	_, err = agent.SetCredential("prn_ada", credential)
	assert.ErrorContains(t, err, "only in brokered mode")
}

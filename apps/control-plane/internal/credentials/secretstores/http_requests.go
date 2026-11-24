package secretstores

import "github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"

type ListSecretStoresRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Secret Stores in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Secret Stores"`
}

type SecretStoreRequest struct {
	SecretStoreID string `path:"secret_store_id" doc:"The Secret Store's ID"`
}

type UpdateSecretStoreRequest struct {
	SecretStoreID string `path:"secret_store_id" doc:"The Secret Store's ID"`
	Body          struct {
		DisplayName    *string       `json:"display_name,omitempty" required:"false" minLength:"1" doc:"New display name"`
		Slug           *string       `json:"slug,omitempty" required:"false" doc:"New slug"`
		Settings       *SettingsBody `json:"settings,omitempty" required:"false" doc:"New settings for the store's kind"`
		Access         *AccessBody   `json:"access,omitempty" required:"false" doc:"New access, replacing the store's"`
		EnvironmentIDs *[]string     `json:"environment_ids,omitempty" required:"false" doc:"The Environments that may read the store, replacing the list; empty allows every Environment"`
	}
}

type CreateSecretStoreRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Secret Store belongs to"`
	Body        struct {
		DisplayName    string        `json:"display_name" required:"true" minLength:"1" doc:"The display name for the store"`
		Slug           *string       `json:"slug,omitempty" required:"false" doc:"Url slug for the store; derived from the display name when omitted"`
		Kind           string        `json:"kind" required:"true" enum:"kubernetes,aws_secrets_manager,vault,doppler,infisical" doc:"The secret manager"`
		Settings       *SettingsBody `json:"settings,omitempty" required:"false" doc:"Where the store is; none for kubernetes"`
		Access         *AccessBody   `json:"access,omitempty" required:"false" doc:"How the store is opened; agent mode with no settings when omitted"`
		EnvironmentIDs []string      `json:"environment_ids,omitempty" required:"false" doc:"The Environments whose Deployments may read the store; every Environment in the Workspace when omitted. Required for doppler"`
	}
}

// SettingsBody says where a store is: only the block for its kind.
type SettingsBody struct {
	AWS       *credentials.AWSSettings       `json:"aws,omitempty" required:"false" doc:"For aws_secrets_manager: the region"`
	Vault     *credentials.VaultSettings     `json:"vault,omitempty" required:"false" doc:"For vault: the address, KV v2 mount, and Enterprise namespace"`
	Doppler   *credentials.DopplerSettings   `json:"doppler,omitempty" required:"false" doc:"For doppler: the project and config; optional when the store is read with a service token, which implies them"`
	Infisical *credentials.InfisicalSettings `json:"infisical,omitempty" required:"false" doc:"For infisical: the site URL (Infisical Cloud's when omitted), project ID, environment slug, and the folder references are relative to (/ when omitted)"`
}

// AccessBody is how a store is opened.
type AccessBody struct {
	Mode       string `json:"mode" required:"true" enum:"agent,federated" doc:"How the store is opened: agent, with the agent's own identity or a credential its operator configured; or federated, with a JWT the control plane signs for each Deployment, which the store trusts"`
	Method     string `json:"method,omitempty" required:"false" enum:"kubernetes,universal_auth,oidc,jwt" doc:"For infisical: how the agent logs in; kubernetes (with identity_id) or universal_auth (with credential) in agent mode, and oidc or jwt (with identity_id) in federated mode"`
	RoleARN    string `json:"role_arn,omitempty" required:"false" doc:"For aws_secrets_manager: the role to assume, on top of the agent's own AWS identity in agent mode, or with the JWT in federated mode, where it is required"`
	Role       string `json:"role,omitempty" required:"false" doc:"For vault: the auth role the agent logs in as"`
	AuthMount  string `json:"auth_mount,omitempty" required:"false" doc:"For vault: the auth method's mount; kubernetes in agent mode and jwt in federated mode when omitted"`
	Credential string `json:"credential,omitempty" required:"false" doc:"For doppler in agent mode, or infisical with universal_auth: the name of the credential, configured on the agent, that holds the Doppler token or the Universal Auth client ID and secret"`
	IdentityID string `json:"identity_id,omitempty" required:"false" doc:"For doppler in federated mode: the service account identity that trusts the federation issuer. For infisical with kubernetes, oidc, or jwt: the machine identity the agent logs in as"`
}

func (b *AccessBody) access() credentials.Access {
	if b == nil {
		return credentials.Access{}
	}
	return credentials.Access{
		Mode:       credentials.AccessMode(b.Mode),
		Method:     credentials.AuthMethod(b.Method),
		RoleARN:    b.RoleARN,
		Role:       b.Role,
		AuthMount:  b.AuthMount,
		Credential: b.Credential,
		IdentityID: b.IdentityID,
	}
}

func (b *SettingsBody) settings() credentials.Settings {
	if b == nil {
		return credentials.Settings{}
	}
	return credentials.Settings{AWS: b.AWS, Vault: b.Vault, Doppler: b.Doppler, Infisical: b.Infisical}
}

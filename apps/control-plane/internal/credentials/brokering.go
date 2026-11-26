package credentials

import (
	"fmt"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// MintingCredential is what a store in brokered mode gives the control
// plane to mint each Deployment's credential with. Only the fields its kind
// takes are set. It never leaves the control plane, is stored only
// encrypted, and is never read back through the API.
type MintingCredential struct {
	// Token is, for doppler, a service account token that can manage the
	// config's service tokens.
	Token string `json:"token,omitempty"`
	// RoleID and SecretID are, for vault, an AppRole's, whose token can
	// create tokens from the store's token role.
	RoleID   string `json:"role_id,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
	// ClientID and ClientSecret are, for infisical, a machine identity's
	// Universal Auth credential.
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// mintingFields are the fields each kind's minting credential needs. A kind
// missing here is brokered without one: aws_secrets_manager, with the
// control plane's own AWS identity.
var mintingFields = map[secretstore.Kind][]string{
	secretstore.KindVault:     {"role_id", "secret_id"},
	secretstore.KindDoppler:   {"token"},
	secretstore.KindInfisical: {"client_id", "client_secret"},
}

// normalize checks that a credential has the fields the kind needs and
// nothing else. Its errors never contain the credential.
func (c MintingCredential) normalize(kind secretstore.Kind) (MintingCredential, error) {
	needs, ok := mintingFields[kind]
	if !ok {
		return MintingCredential{}, NewErrInvalidInput(fmt.Sprintf("a %s store is brokered without a credential", kind))
	}
	given := map[string]string{
		"token": c.Token, "role_id": c.RoleID, "secret_id": c.SecretID,
		"client_id": c.ClientID, "client_secret": c.ClientSecret,
	}
	for _, name := range slices.Sorted(maps.Keys(given)) {
		value := given[name]
		switch {
		case value == "" && slices.Contains(needs, name):
			return MintingCredential{}, NewErrInvalidInput(fmt.Sprintf("a %s store's credential needs %s", kind, name))
		case value != "" && !slices.Contains(needs, name):
			return MintingCredential{}, NewErrInvalidInput(fmt.Sprintf("a %s store's credential takes no %s", kind, name))
		case utf8.RuneCountInString(value) > maxMintingCredentialLength:
			return MintingCredential{}, NewErrInvalidInput(fmt.Sprintf("%s can have at most %d characters", name, maxMintingCredentialLength))
		}
	}
	return c, nil
}

// BrokeredCredential is a credential minted for one Deployment: a token, or
// AWS session credentials. It goes to the agent and nowhere else.
type BrokeredCredential struct {
	Token           string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// MintRequest asks a Broker for one Deployment's credential for one store.
type MintRequest struct {
	Store      *SecretStore
	Credential MintingCredential
	// DeploymentID names what is minted where the store keeps a name.
	DeploymentID string
	// Paths are the paths of the Deployment's secrets in the store, with
	// their bindings filled in. A store that can scope a credential to
	// secrets, such as AWS, scopes it to these.
	Paths []string
}

// Minted is what a Broker minted.
type Minted struct {
	Credential BrokeredCredential
	ExpiresAt  time.Time
	// ProviderID is the store's name for what was minted, such as a Doppler
	// token's slug or a Vault token's accessor, which revokes it; never the
	// credential.
	ProviderID string
}

// RevokeRequest asks a Broker to revoke what it minted.
type RevokeRequest struct {
	Store      *SecretStore
	Credential MintingCredential
	ProviderID string
}

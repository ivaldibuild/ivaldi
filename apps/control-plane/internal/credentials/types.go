package credentials

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/pointers"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// Settings say where a Secret Store is, whoever reads it. Only the block
// for the store's kind is set; a kubernetes store has none, and a doppler
// store may have none.
type Settings struct {
	AWS     *AWSSettings     `json:"aws,omitempty"`
	Vault   *VaultSettings   `json:"vault,omitempty"`
	Doppler *DopplerSettings `json:"doppler,omitempty"`
}

// clone returns a deep copy of the Settings.
func (s Settings) clone() Settings {
	return Settings{AWS: pointers.Copy(s.AWS), Vault: pointers.Copy(s.Vault), Doppler: pointers.Copy(s.Doppler)}
}

// equal reports whether two Settings are equal.
func (s Settings) equal(o Settings) bool {
	return pointers.Equal(s.AWS, o.AWS) && pointers.Equal(s.Vault, o.Vault) && pointers.Equal(s.Doppler, o.Doppler)
}

// AWSSettings locate secrets in AWS Secrets Manager.
type AWSSettings struct {
	Region string `json:"region"`
}

// VaultSettings locate secrets in a Vault KV v2 mount.
type VaultSettings struct {
	Address string `json:"address"`
	Mount   string `json:"mount"`
	// Namespace is a Vault Enterprise namespace.
	Namespace string `json:"namespace,omitempty"`
}

// DopplerSettings name the Doppler config a store reads. A service token
// implies its config, so a store read with one may leave them out.
type DopplerSettings struct {
	Project string `json:"project"`
	Config  string `json:"config"`
}

// AccessMode is how a Secret Store is opened.
type AccessMode string

// AccessAgent has the agent open the store with its own identity, or with a
// credential its operator configured under the name the access gives.
const AccessAgent AccessMode = "agent"

// Access is how a Secret Store is opened: its mode, and the settings that
// mode needs for the store's kind. It never holds a credential.
type Access struct {
	Mode AccessMode `json:"mode" enum:"agent"`
	// RoleARN is, for aws_secrets_manager, a role assumed on top of the
	// agent's own AWS identity.
	RoleARN string `json:"role_arn,omitempty"`
	// Role is, for vault, the auth role the agent logs in as.
	Role string `json:"role,omitempty"`
	// AuthMount is, for vault, the auth method's mount; empty means
	// "kubernetes".
	AuthMount string `json:"auth_mount,omitempty"`
	// Credential is, for doppler, the name of the credential the agent's
	// operator configured, which holds a Doppler token.
	Credential string `json:"credential,omitempty"`
}

// normalize checks that an access has a mode and the settings that mode
// needs for the kind, and nothing else. An empty mode is agent.
func (a Access) normalize(kind secretstore.Kind) (Access, error) {
	if a.Mode == "" {
		a.Mode = AccessAgent
	}
	if a.Mode != AccessAgent {
		return Access{}, NewErrInvalidInput(fmt.Sprintf("access mode %q must be agent", a.Mode))
	}
	// The fields each kind takes in agent mode; any other set field is
	// refused, so a setting is never silently ignored.
	takes := map[secretstore.Kind][]string{
		secretstore.KindAWSSecretsManager: {"role_arn"},
		secretstore.KindVault:             {"role", "auth_mount"},
		secretstore.KindDoppler:           {"credential"},
	}[kind]

	given := map[string]string{"role_arn": a.RoleARN, "role": a.Role, "auth_mount": a.AuthMount, "credential": a.Credential}

	for _, name := range []string{"role_arn", "role", "auth_mount", "credential"} {
		if given[name] != "" && !slices.Contains(takes, name) {
			return Access{}, NewErrInvalidInput(fmt.Sprintf("a %s store in %s mode takes no %s", kind, a.Mode, name))
		}
		if utf8.RuneCountInString(given[name]) > maxSettingLength {
			return Access{}, NewErrInvalidInput(fmt.Sprintf("%s can have at most %d characters", name, maxSettingLength))
		}
	}

	switch kind {
	case secretstore.KindVault:
		if a.Role == "" {
			return Access{}, NewErrInvalidInput("a vault store in agent mode needs the auth role the agent logs in as")
		}
	case secretstore.KindDoppler:
		if a.Credential == "" {
			return Access{}, NewErrInvalidInput("a doppler store in agent mode needs the name of the credential that holds its token")
		}

		if len(a.Credential) > maxCredentialNameLength || !credentialName.MatchString(a.Credential) {
			return Access{}, NewErrInvalidInput(fmt.Sprintf(
				"credential %q must be at most %d lowercase letters, digits, '-', and '.', starting and ending with a letter or digit",
				a.Credential, maxCredentialNameLength))
		}
	}

	return a, nil
}

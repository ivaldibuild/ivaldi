package credentials

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/pointers"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// Settings say where a Secret Store is, whoever reads it. Only the block
// for the store's kind is set; a kubernetes store has none, and a doppler
// store may have none.
type Settings struct {
	AWS       *AWSSettings       `json:"aws,omitempty"`
	Vault     *VaultSettings     `json:"vault,omitempty"`
	Doppler   *DopplerSettings   `json:"doppler,omitempty"`
	Infisical *InfisicalSettings `json:"infisical,omitempty"`
}

// clone returns a deep copy of the Settings.
func (s Settings) clone() Settings {
	return Settings{
		AWS: pointers.Copy(s.AWS), Vault: pointers.Copy(s.Vault), Doppler: pointers.Copy(s.Doppler),
		Infisical: pointers.Copy(s.Infisical),
	}
}

// equal reports whether two Settings are equal.
func (s Settings) equal(o Settings) bool {
	return pointers.Equal(s.AWS, o.AWS) && pointers.Equal(s.Vault, o.Vault) && pointers.Equal(s.Doppler, o.Doppler) &&
		pointers.Equal(s.Infisical, o.Infisical)
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

// InfisicalSettings locate secrets in one environment of an Infisical
// project, under one folder.
type InfisicalSettings struct {
	// SiteURL is Infisical's address: Infisical Cloud's, by default, or a
	// self-hosted Infisical's.
	SiteURL   string `json:"site_url"`
	ProjectID string `json:"project_id"`
	// Environment is the environment's slug.
	Environment string `json:"environment"`
	// SecretPath is the folder a reference's path is relative to; "/" by
	// default.
	SecretPath string `json:"secret_path"`
}

// AccessMode is how a Secret Store is opened.
type AccessMode string

const (
	// AccessAgent has the agent open the store with its own identity, or
	// with a credential its operator configured under the name the access
	// gives.
	AccessAgent AccessMode = "agent"
	// AccessFederated has the control plane sign a short-lived JWT for each
	// Deployment, which the agent exchanges at the store, whose own
	// configuration trusts the control plane's federation issuer.
	AccessFederated AccessMode = "federated"
)

// AuthMethod is how a store whose kind has several ways to log in is
// logged in to, in its mode. Only infisical has several.
type AuthMethod string

const (
	// AuthKubernetes logs in with the agent's service-account token, which
	// the store checks with the cluster.
	AuthKubernetes AuthMethod = "kubernetes"
	// AuthUniversal logs in with a client ID and secret held in a named
	// credential.
	AuthUniversal AuthMethod = "universal_auth"
	// AuthOIDC logs in with the federated JWT, which the store checks
	// against the issuer's discovery document.
	AuthOIDC AuthMethod = "oidc"
	// AuthJWT logs in with the federated JWT, which the store checks
	// against the issuer's keys configured on it.
	AuthJWT AuthMethod = "jwt"
)

// Access is how a Secret Store is opened: its mode, and the settings that
// mode needs for the store's kind. It never holds a credential.
type Access struct {
	Mode AccessMode `json:"mode" enum:"agent,federated"`
	// Method is, for infisical, how the agent logs in: kubernetes or
	// universal_auth in agent mode, and oidc or jwt in federated mode.
	Method AuthMethod `json:"method,omitempty" enum:"kubernetes,universal_auth,oidc,jwt"`
	// RoleARN is, for aws_secrets_manager, a role assumed on top of the
	// agent's own AWS identity in agent mode, or with the federated JWT.
	RoleARN string `json:"role_arn,omitempty"`
	// Role is, for vault, the auth role the agent logs in as.
	Role string `json:"role,omitempty"`
	// AuthMount is, for vault, the auth method's mount; empty means
	// "kubernetes" in agent mode and "jwt" in federated mode.
	AuthMount string `json:"auth_mount,omitempty"`
	// Credential is the name of the credential the agent's operator
	// configured: for doppler in agent mode, holding a Doppler token; for
	// infisical with universal_auth, a Universal Auth client ID and secret.
	Credential string `json:"credential,omitempty"`
	// IdentityID is, for doppler in federated mode, the service account
	// identity that trusts the federation issuer; for infisical, the machine
	// identity the agent logs in as.
	IdentityID string `json:"identity_id,omitempty"`
}

// accessRule is what one kind takes, and needs, in one mode.
type accessRule struct {
	takes []string
	needs map[string]string
	// methods, for a kind with several ways to log in, are the rule for
	// each: the access then needs a method, and follows its rule.
	methods map[AuthMethod]accessRule
}

// accessRules are the fields each kind takes in each mode, and the ones it
// needs, with what each is for. A kind missing from a mode cannot use it.
var accessRules = map[AccessMode]map[secretstore.Kind]accessRule{
	AccessAgent: {
		secretstore.KindKubernetes:        {},
		secretstore.KindAWSSecretsManager: {takes: []string{"role_arn"}},
		secretstore.KindVault: {takes: []string{"role", "auth_mount"},
			needs: map[string]string{"role": "the auth role the agent logs in as"}},
		secretstore.KindDoppler: {takes: []string{"credential"},
			needs: map[string]string{"credential": "the name of the credential that holds its token"}},
		secretstore.KindInfisical: {methods: map[AuthMethod]accessRule{
			AuthKubernetes: {takes: []string{"identity_id"},
				needs: map[string]string{"identity_id": "the identity_id of the machine identity whose Kubernetes auth the agent logs in with"}},
			AuthUniversal: {takes: []string{"credential"},
				needs: map[string]string{"credential": "the name of the credential that holds its Universal Auth client ID and secret"}},
		}},
	},
	AccessFederated: {
		secretstore.KindAWSSecretsManager: {takes: []string{"role_arn"},
			needs: map[string]string{"role_arn": "the role_arn of the role that trusts the federation issuer"}},
		secretstore.KindVault: {takes: []string{"role", "auth_mount"},
			needs: map[string]string{"role": "the JWT auth role the agent logs in as"}},
		secretstore.KindDoppler: {takes: []string{"identity_id"},
			needs: map[string]string{"identity_id": "the identity_id of the service account identity that trusts the federation issuer"}},
		secretstore.KindInfisical: {methods: map[AuthMethod]accessRule{
			AuthOIDC: {takes: []string{"identity_id"},
				needs: map[string]string{"identity_id": "the identity_id of the machine identity whose OIDC auth trusts the federation issuer"}},
			AuthJWT: {takes: []string{"identity_id"},
				needs: map[string]string{"identity_id": "the identity_id of the machine identity whose JWT auth holds the federation issuer's keys"}},
		}},
	},
}

// normalize checks that an access has a mode the kind can use, the
// settings that mode needs, and nothing else. An empty mode is agent.
func (a Access) normalize(kind secretstore.Kind, settings Settings) (Access, error) {
	if a.Mode == "" {
		a.Mode = AccessAgent
	}
	kinds, ok := accessRules[a.Mode]
	if !ok {
		return Access{}, NewErrInvalidInput(fmt.Sprintf("access mode %q must be agent or federated", a.Mode))
	}
	rule, ok := kinds[kind]
	if !ok {
		if kind == secretstore.KindKubernetes {
			return Access{}, NewErrInvalidInput("a kubernetes store is always in agent mode: the agent's RBAC is its grant")
		}
		return Access{}, NewErrInvalidInput(fmt.Sprintf("a %s store cannot be in %s mode", kind, a.Mode))
	}
	if rule.methods != nil {
		method, ok := rule.methods[a.Method]
		if !ok {
			names := make([]string, 0, len(rule.methods))
			for m := range rule.methods {
				names = append(names, string(m))
			}
			slices.Sort(names)
			return Access{}, NewErrInvalidInput(fmt.Sprintf("a %s store in %s mode logs in with method %s",
				kind, a.Mode, strings.Join(names, " or ")))
		}
		rule = accessRule{takes: append([]string{"method"}, method.takes...), needs: method.needs}
	}

	// Any set field the kind does not take in this mode is refused, so a
	// setting is never silently ignored.
	given := map[string]string{
		"role_arn": a.RoleARN, "role": a.Role, "auth_mount": a.AuthMount,
		"credential": a.Credential, "identity_id": a.IdentityID, "method": string(a.Method),
	}
	for _, name := range slices.Sorted(maps.Keys(given)) {
		if given[name] != "" && !slices.Contains(rule.takes, name) {
			return Access{}, NewErrInvalidInput(fmt.Sprintf("a %s store in %s mode takes no %s", kind, a.Mode, name))
		}
		if utf8.RuneCountInString(given[name]) > maxSettingLength {
			return Access{}, NewErrInvalidInput(fmt.Sprintf("%s can have at most %d characters", name, maxSettingLength))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(rule.needs)) {
		if given[name] == "" {
			return Access{}, NewErrInvalidInput(fmt.Sprintf("a %s store in %s mode needs %s", kind, a.Mode, rule.needs[name]))
		}
	}

	if a.Credential != "" && (len(a.Credential) > maxCredentialNameLength || !credentialName.MatchString(a.Credential)) {
		return Access{}, NewErrInvalidInput(fmt.Sprintf(
			"credential %q must be at most %d lowercase letters, digits, '-', and '.', starting and ending with a letter or digit",
			a.Credential, maxCredentialNameLength))
	}
	// A service account identity's token reads any config its account may,
	// so the store must name the one it serves.
	if kind == secretstore.KindDoppler && a.Mode == AccessFederated && settings.Doppler == nil {
		return Access{}, NewErrInvalidInput("a doppler store in federated mode needs the Doppler project and config it reads")
	}
	return a, nil
}

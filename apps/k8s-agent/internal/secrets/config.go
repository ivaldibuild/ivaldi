package secrets

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// The access modes this agent can open stores in, for the heartbeat.
var Modes = []string{"agent", "federated", "brokered"}

// The kinds this agent can read, for the heartbeat.
var Kinds = []string{
	string(secretstore.KindKubernetes), string(secretstore.KindAWSSecretsManager),
	string(secretstore.KindVault), string(secretstore.KindDoppler), string(secretstore.KindInfisical),
}

// Config configures the Resolver.
type Config struct {
	Kube kubernetes.Interface
	// AgentNamespace holds the fingerprint key and credential Secrets.
	AgentNamespace    string
	FingerprintSecret string
	// Credentials are the credentials the agent's operator configured, by
	// the name a Secret Store's access gives. A store names a credential,
	// never where it is kept, so the agent reads only Secrets its operator
	// mapped.
	Credentials map[string]Credential
	// Addresses are the servers, such as a Vault, the agent's operator lets
	// it log in to. A store's address comes from the control plane, where
	// whoever manages a Workspace's stores sets it, and a login sends a
	// token or a federated JWT there: a store at any other address is
	// refused. Doppler's API and AWS STS are always allowed, because a
	// store cannot name another address for them, and so is Infisical
	// Cloud.
	Addresses []string
	// VaultTokenFile is the token Vault's Kubernetes auth checks: one
	// projected for Vault's audience, never the Pod's API token.
	VaultTokenFile string
	// InfisicalTokenFile is the token Infisical's Kubernetes auth checks,
	// projected for Infisical's audience.
	InfisicalTokenFile string
	// Grants asks the control plane for access to the stores in federated
	// mode a Deployment reads. Its answer holds credentials.
	Grants func(ctx context.Context, deploymentID string) ([]apiclient.GrantView, error)
	Masker *Masker
	Logger *slog.Logger
}

// allows reports whether the operator listed address.
func (c Config) allows(address string) bool {
	normalize := func(a string) string { return strings.TrimRight(strings.TrimSpace(a), "/") }
	return slices.ContainsFunc(c.Addresses, func(allowed string) bool {
		return normalize(allowed) != "" && normalize(allowed) == normalize(address)
	})
}

// allowsInfisical reports whether the agent may log in to an Infisical at
// site: Infisical Cloud, or a self-hosted one the operator listed.
func (c Config) allowsInfisical(site string) bool {
	return slices.Contains(secretstore.InfisicalSites, strings.TrimRight(strings.TrimSpace(site), "/")) || c.allows(site)
}

// StoreKinds lists the kinds of secret store an agent with this
// configuration can read: every kind, less vault when no address is
// allowed, since a vault store always names its own.
func (c Config) StoreKinds() []string {
	return slices.DeleteFunc(slices.Clone(Kinds), func(kind string) bool {
		return kind == string(secretstore.KindVault) && len(c.Addresses) == 0
	})
}

// StoreModes lists the access modes an agent with this configuration can
// open stores in: agent mode always, and federated and brokered when it can
// ask the control plane for grants.
func (c Config) StoreModes() []string {
	if c.Grants == nil {
		return []string{"agent"}
	}
	return slices.Clone(Modes)
}

// CredentialNames lists the configured credentials' names, for the
// heartbeat: never where they are kept or what they hold.
func (c Config) CredentialNames() []string {
	return slices.Sorted(maps.Keys(c.Credentials))
}

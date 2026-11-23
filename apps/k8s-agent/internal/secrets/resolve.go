package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// The kinds this agent can read, for the heartbeat.
var Kinds = []string{
	string(secretstore.KindKubernetes), string(secretstore.KindAWSSecretsManager),
	string(secretstore.KindVault), string(secretstore.KindDoppler),
}

// retries is how many times a transient store failure is tried in all.
const retries = 4

// Resolved is one secret resolved for a Deployment. Value never leaves the
// agent; the rest is reported as the Deployment's pin.
type Resolved struct {
	Name string
	// Path is the secret's path with its bindings filled in, as it was read.
	Path        string
	Value       string
	Version     string
	Fingerprint string
	ValuesPath  string
	AsValue     bool
}

// Pin is what the Deployment records about the secret: never the value.
func (r Resolved) Pin() deploy.SecretPin {
	deliveredAs := "secret"
	if r.AsValue {
		deliveredAs = "value"
	}
	return deploy.SecretPin{
		Name: r.Name, Path: r.Path, Version: r.Version, Fingerprint: r.Fingerprint, DeliveredAs: deliveredAs,
	}
}

// Resolver resolves a Deployment's secrets.
type Resolver interface {
	// Resolve reads secrets for a Deployment in namespace, fingerprints
	// them, and checks pinned fingerprints. Every value is masked from then
	// on, until Release. A failure is a *deploy.Failure naming the secret
	// and its reference, never its value.
	Resolve(ctx context.Context, namespace string, secrets []apiclient.AssignmentSecret) ([]Resolved, error)
	// Release stops masking values that are no longer in use.
	Release(resolved []Resolved)
}

// Credential is where a named credential is kept: a key of a Secret in the
// agent's namespace.
type Credential struct {
	Secret string
	Key    string
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
	// VaultAddresses are the Vault servers the agent's operator lets it log
	// in to. A store's address comes from the control plane, where whoever
	// manages a Workspace's stores sets it, and a login sends a
	// service-account token there: a store at any other address is refused.
	VaultAddresses []string
	// VaultTokenFile is the token Vault's Kubernetes auth checks: one
	// projected for Vault's audience, never the Pod's API token.
	VaultTokenFile string
	Masker         *Masker
	Logger         *slog.Logger
}

// allowsVault reports whether the operator listed address.
func (c Config) allowsVault(address string) bool {
	normalize := func(a string) string { return strings.TrimRight(strings.TrimSpace(a), "/") }
	return slices.ContainsFunc(c.VaultAddresses, func(allowed string) bool {
		return normalize(allowed) != "" && normalize(allowed) == normalize(address)
	})
}

// StoreKinds lists the kinds of secret store an agent with this
// configuration can read: every kind, less vault when no address is allowed.
func (c Config) StoreKinds() []string {
	return slices.DeleteFunc(slices.Clone(Kinds), func(kind string) bool {
		return kind == string(secretstore.KindVault) && len(c.VaultAddresses) == 0
	})
}

// CredentialNames lists the configured credentials' names, for the
// heartbeat: never where they are kept or what they hold.
func (c Config) CredentialNames() []string {
	return slices.Sorted(maps.Keys(c.Credentials))
}

// NewResolver returns a Resolver. The fingerprint keys are read for each
// Deployment that uses a secret.
func NewResolver(cfg Config) Resolver {
	r := &resolver{cfg: cfg, retryBase: time.Second}
	r.store = r.newStore
	r.loadKeys = func(ctx context.Context) (Fingerprinter, error) {
		return LoadFingerprinter(ctx, cfg.Kube, cfg.AgentNamespace, cfg.FingerprintSecret)
	}
	return r
}

type resolver struct {
	cfg       Config
	retryBase time.Duration
	store     func(ctx context.Context, settings apiclient.StoreSettings, namespace string) (secretstore.Store, error)
	loadKeys  func(ctx context.Context) (Fingerprinter, error)
}

// fingerprinter reads the key ring afresh for each Deployment, so a key an
// operator adds is used, and a key removed is refused, without a restart.
func (r *resolver) fingerprinter(ctx context.Context) (Fingerprinter, error) {
	return r.loadKeys(ctx)
}

func (r *resolver) Resolve(ctx context.Context, namespace string, secrets []apiclient.AssignmentSecret) ([]Resolved, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	keys, err := r.fingerprinter(ctx)
	if err != nil {
		return nil, &deploy.Failure{Reason: "could not load the fingerprint key", Err: err}
	}

	// Each store is read once, with every reference it holds.
	type group struct {
		settings apiclient.StoreSettings
		indexes  []int
	}
	var order []string
	groups := map[string]*group{}
	for i, s := range secrets {
		g, ok := groups[s.Store.Id]
		if !ok {
			g = &group{settings: s.Store}
			groups[s.Store.Id] = g
			order = append(order, s.Store.Id)
		}
		g.indexes = append(g.indexes, i)
	}

	out := make([]Resolved, len(secrets))
	var added []string
	for _, id := range order {
		g := groups[id]
		store, err := r.store(ctx, g.settings, namespace)
		if err != nil {
			r.cfg.Masker.Remove(added...)
			return nil, err
		}
		refs := make([]secretstore.Ref, len(g.indexes))
		for j, i := range g.indexes {
			refs[j] = ref(secrets[i].Ref)
		}
		values, err := r.getMany(ctx, store, refs)
		if err != nil {
			r.cfg.Masker.Remove(added...)
			return nil, r.failure(secrets, g.indexes, err)
		}
		for j, i := range g.indexes {
			s, v := secrets[i], values[j]
			r.cfg.Masker.Add(v.Value)
			added = append(added, v.Value)
			resolved := Resolved{
				Name: s.Name, Path: s.Ref.Path, Value: v.Value, Version: v.Version, Fingerprint: keys.Fingerprint(v.Value),
				AsValue: s.DeliverAs == apiclient.AssignmentSecretDeliverAsValue,
			}
			if s.ValuesPath != nil {
				resolved.ValuesPath = *s.ValuesPath
			}
			if s.Fingerprint != nil && *s.Fingerprint != "" {
				same, err := keys.Matches(*s.Fingerprint, v.Value)
				if err != nil {
					r.cfg.Masker.Remove(added...)
					return nil, &deploy.Failure{Reason: fmt.Sprintf("secret %s cannot be checked against the Deployment it restores", s.Name), Err: err}
				}
				if !same {
					r.cfg.Masker.Remove(added...)
					return nil, deploy.Failf("secret %s (%s) has changed since the Deployment this restores; request the rollback with current secrets to use it",
						s.Name, ref(s.Ref))
				}
			}
			out[i] = resolved
		}
	}
	return out, nil
}

func (r *resolver) Release(resolved []Resolved) {
	for _, s := range resolved {
		r.cfg.Masker.Remove(s.Value)
	}
}

// getMany reads refs, retrying transient failures.
func (r *resolver) getMany(ctx context.Context, store secretstore.Store, refs []secretstore.Ref) ([]secretstore.Value, error) {
	wait := r.retryBase
	for attempt := 1; ; attempt++ {
		values, err := secretstore.GetMany(ctx, store, refs)
		if err == nil || !secretstore.Transient(err) || attempt == retries {
			return values, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// failure names the secret a store error is about.
func (r *resolver) failure(secrets []apiclient.AssignmentSecret, indexes []int, err error) error {
	var storeErr *secretstore.Error
	name := secrets[indexes[0]].Name
	if errors.As(err, &storeErr) {
		for _, i := range indexes {
			if ref(secrets[i].Ref) == storeErr.Ref {
				name = secrets[i].Name
			}
		}
	}
	return &deploy.Failure{Reason: fmt.Sprintf("could not resolve secret %s", name), Err: err}
}

func ref(r apiclient.SecretRef) secretstore.Ref {
	out := secretstore.Ref{Path: r.Path}
	if r.Field != nil {
		out.Field = *r.Field
	}
	if r.Version != nil {
		out.Version = *r.Version
	}
	return out
}

// newStore builds the store a Deployment's secret names. Stores are built per
// Deployment and never cached across them.
func (r *resolver) newStore(ctx context.Context, s apiclient.StoreSettings, namespace string) (secretstore.Store, error) {
	if s.Access.Mode != apiclient.StoreAccessModeAgent {
		return nil, deploy.Failf("secret store %s is opened in %q mode, which this agent cannot use", s.Id, s.Access.Mode)
	}
	switch s.Kind {
	case apiclient.StoreSettingsKindKubernetes:
		return secretstore.NewKubernetes(r.kubernetesGetter(namespace)), nil
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		api, err := secretstore.NewAWSClient(ctx, s.Aws.Region, deref(s.Access.RoleArn))
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		if s.Vault == nil {
			return nil, deploy.Failf("secret store %s has no Vault settings", s.Id)
		}
		if !r.cfg.allowsVault(s.Vault.Address) {
			return nil, deploy.Failf("secret store %s is a Vault at %s, which this agent is not set up to log in to; its operator lists the addresses it may use in the chart's secrets.vault.addresses",
				s.Id, s.Vault.Address)
		}
		return secretstore.NewVault(secretstore.VaultConfig{
			Address: s.Vault.Address, Mount: s.Vault.Mount, Namespace: deref(s.Vault.Namespace),
			AuthRole: deref(s.Access.Role), AuthMount: deref(s.Access.AuthMount),
			JWT: func() (string, error) {
				raw, err := os.ReadFile(r.cfg.VaultTokenFile)
				return strings.TrimSpace(string(raw)), err
			},
		}), nil
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		name := deref(s.Access.Credential)
		credential, ok := r.cfg.Credentials[name]
		if !ok {
			return nil, deploy.Failf("secret store %s needs the credential %q, which this agent's operator has not configured; credentials are mapped to Secrets in the chart's secrets.credentials",
				s.Id, name)
		}
		return secretstore.NewDoppler(secretstore.DopplerConfig{
			Project: deref(s.Doppler.Project), Config: deref(s.Doppler.Config),
			Token: func(ctx context.Context) (string, error) { return r.credential(ctx, name, credential) },
		}), nil
	}
	return nil, deploy.Failf("this agent cannot read %q secret stores", s.Kind)
}

// credential reads a named credential from its Secret in the agent's
// namespace, each time it is needed, so a rotated one is used at once.
func (r *resolver) credential(ctx context.Context, name string, c Credential) (string, error) {
	data, _, err := r.kubernetesGetter(r.cfg.AgentNamespace)(ctx, c.Secret)
	if err != nil {
		return "", fmt.Errorf("credential %s, in Secret %s: %w", name, c.Secret, err)
	}
	value := strings.TrimSpace(string(data[c.Key]))
	if value == "" {
		return "", fmt.Errorf("credential %s: Secret %s has no key %s", name, c.Secret, c.Key)
	}
	return value, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// kubernetesGetter reads Secrets in namespace with the agent's own RBAC.
func (r *resolver) kubernetesGetter(namespace string) secretstore.SecretGetter {
	return func(ctx context.Context, name string) (map[string][]byte, string, error) {
		secret, err := r.cfg.Kube.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			return nil, "", secretstore.ErrNotFound
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			return nil, "", fmt.Errorf("%w: RBAC denies get on secrets in %s", secretstore.ErrDenied, namespace)
		case err != nil:
			return nil, "", fmt.Errorf("%w: %v", secretstore.ErrUnavailable, err)
		}
		return secret.Data, secret.ResourceVersion, nil
	}
}

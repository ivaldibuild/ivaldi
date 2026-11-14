package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	Name        string
	Value       string
	Version     string
	Fingerprint string
	ValuesPath  string
	AsValue     bool
}

// Pin is what the Deployment records about the secret: never the value.
func (r Resolved) Pin() map[string]any {
	out := map[string]any{"name": r.Name, "fingerprint": r.Fingerprint, "delivered_as": "secret"}
	if r.AsValue {
		out["delivered_as"] = "value"
	}
	if r.Version != "" {
		out["version"] = r.Version
	}
	return out
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

// Config configures the Resolver.
type Config struct {
	Kube kubernetes.Interface
	// AgentNamespace holds the fingerprint key and Doppler token Secrets.
	AgentNamespace    string
	FingerprintSecret string
	// ServiceAccountTokenFile is the token Vault's Kubernetes auth checks.
	ServiceAccountTokenFile string
	Masker                  *Masker
	Logger                  *slog.Logger
}

// NewResolver returns a Resolver. The fingerprint key is loaded the first
// time a Deployment uses a secret.
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

	mu   sync.Mutex
	keys Fingerprinter
}

func (r *resolver) fingerprinter(ctx context.Context) (Fingerprinter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys != nil {
		return r.keys, nil
	}
	keys, err := r.loadKeys(ctx)
	if err != nil {
		return nil, err
	}
	r.keys = keys
	return keys, nil
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
				Name: s.Name, Value: v.Value, Version: v.Version, Fingerprint: keys.Fingerprint(v.Value),
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
	switch s.Kind {
	case apiclient.StoreSettingsKindKubernetes:
		return secretstore.NewKubernetes(r.kubernetesGetter(namespace)), nil
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		roleARN := ""
		if s.Aws.RoleArn != nil {
			roleARN = *s.Aws.RoleArn
		}
		api, err := secretstore.NewAWSClient(ctx, s.Aws.Region, roleARN)
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		if s.Vault == nil {
			return nil, deploy.Failf("secret store %s has no Vault settings", s.Id)
		}
		cfg := secretstore.VaultConfig{
			Address: s.Vault.Address, Mount: s.Vault.Mount, AuthRole: s.Vault.AuthRole,
			JWT: func() (string, error) {
				raw, err := os.ReadFile(r.cfg.ServiceAccountTokenFile)
				return strings.TrimSpace(string(raw)), err
			},
		}
		if s.Vault.AuthMount != nil {
			cfg.AuthMount = *s.Vault.AuthMount
		}
		if s.Vault.Namespace != nil {
			cfg.Namespace = *s.Vault.Namespace
		}
		return secretstore.NewVault(cfg), nil
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		name, key := s.Doppler.TokenSecretName, s.Doppler.TokenSecretKey
		return secretstore.NewDoppler(secretstore.DopplerConfig{Token: func(ctx context.Context) (string, error) {
			data, _, err := r.kubernetesGetter(r.cfg.AgentNamespace)(ctx, name)
			if err != nil {
				return "", fmt.Errorf("the Doppler token Secret %s: %w", name, err)
			}
			token := strings.TrimSpace(string(data[key]))
			if token == "" {
				return "", fmt.Errorf("the Doppler token Secret %s has no key %s", name, key)
			}
			return token, nil
		}}), nil
	}
	return nil, deploy.Failf("this agent cannot read %q secret stores", s.Kind)
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

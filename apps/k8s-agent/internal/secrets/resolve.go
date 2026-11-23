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

// ErrNotApplying is returned when the control plane will grant nothing for
// a Deployment because it is no longer applying: it was cancelled, or
// another agent finished it. The apply is dropped without a report.
var ErrNotApplying = errors.New("the deployment is no longer applying")

// Resolver resolves a Deployment's secrets.
type Resolver interface {
	// Resolve reads secrets for a Deployment in namespace, fingerprints
	// them, and checks pinned fingerprints. A store in federated mode is
	// opened with access the control plane grants for the Deployment.
	// Every value is masked from then on, until Release. A failure is a
	// *deploy.Failure naming the secret and its reference, never its value,
	// or ErrNotApplying.
	Resolve(ctx context.Context, deploymentID, namespace string, secrets []apiclient.AssignmentSecret) ([]Resolved, error)
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
	// Addresses are the servers, such as a Vault, the agent's operator lets
	// it log in to. A store's address comes from the control plane, where
	// whoever manages a Workspace's stores sets it, and a login sends a
	// token or a federated JWT there: a store at any other address is
	// refused. Doppler's API and AWS STS are always allowed, because a
	// store cannot name another address for them.
	Addresses []string
	// VaultTokenFile is the token Vault's Kubernetes auth checks: one
	// projected for Vault's audience, never the Pod's API token.
	VaultTokenFile string
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

// StoreKinds lists the kinds of secret store an agent with this
// configuration can read: every kind, less vault when no address is
// allowed, since a vault store always names its own.
func (c Config) StoreKinds() []string {
	return slices.DeleteFunc(slices.Clone(Kinds), func(kind string) bool {
		return kind == string(secretstore.KindVault) && len(c.Addresses) == 0
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
	r := &resolver{cfg: cfg, retryBase: time.Second, grantRetryBase: time.Second}
	r.store = r.newStore
	r.loadKeys = func(ctx context.Context) (Fingerprinter, error) {
		return LoadFingerprinter(ctx, cfg.Kube, cfg.AgentNamespace, cfg.FingerprintSecret)
	}
	return r
}

type resolver struct {
	cfg            Config
	retryBase      time.Duration
	grantRetryBase time.Duration
	store          func(ctx context.Context, settings apiclient.StoreSettings, open opening) (secretstore.Store, error)
	loadKeys       func(ctx context.Context) (Fingerprinter, error)
}

// opening is what a store is opened for: a Deployment in a namespace, with
// the JWT the control plane granted when the store is federated. mask masks
// a credential the store gets in exchange, until the values are read.
type opening struct {
	deploymentID string
	namespace    string
	jwt          string
	mask         func(string)
}

// fingerprinter reads the key ring afresh for each Deployment, so a key an
// operator adds is used, and a key removed is refused, without a restart.
func (r *resolver) fingerprinter(ctx context.Context) (Fingerprinter, error) {
	return r.loadKeys(ctx)
}

func (r *resolver) Resolve(ctx context.Context, deploymentID, namespace string, secrets []apiclient.AssignmentSecret) ([]Resolved, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	keys, err := r.fingerprinter(ctx)
	if err != nil {
		return nil, &deploy.Failure{Reason: "could not load the fingerprint key", Err: err}
	}
	jwts, err := r.grants(ctx, deploymentID, secrets)
	if err != nil {
		return nil, err
	}
	// A JWT, and what a store gives in exchange for it, is used only to
	// open its store, below, so each is masked only until the values are
	// read.
	var credentials []string
	mask := func(c string) {
		r.cfg.Masker.Add(c)
		credentials = append(credentials, c)
	}
	for _, jwt := range jwts {
		mask(jwt)
	}
	defer func() { r.cfg.Masker.Remove(credentials...) }()

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
		store, err := r.store(ctx, g.settings, opening{deploymentID: deploymentID, namespace: namespace, jwt: jwts[id], mask: mask})
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

// grants asks the control plane for access to the federated stores among
// the secrets', and returns each store's JWT by its ID. It asks nothing when
// none is federated, and retries while the control plane is unreachable.
func (r *resolver) grants(ctx context.Context, deploymentID string, secrets []apiclient.AssignmentSecret) (map[string]string, error) {
	if !slices.ContainsFunc(secrets, func(s apiclient.AssignmentSecret) bool {
		return s.Store.Access.Mode == apiclient.StoreAccessModeFederated
	}) {
		return nil, nil
	}
	if r.cfg.Grants == nil {
		return nil, deploy.Failf("this agent cannot ask for access to federated secret stores")
	}
	var grants []apiclient.GrantView
	wait := r.grantRetryBase
	for attempt := 1; ; attempt++ {
		var err error
		grants, err = r.cfg.Grants(ctx, deploymentID)
		if err == nil {
			break
		}
		switch {
		case errors.Is(err, apiclient.ErrConflict):
			return nil, fmt.Errorf("%w: %v", ErrNotApplying, err)
		case !apiclient.Transient(err) || attempt == retries:
			return nil, &deploy.Failure{Reason: "the control plane granted no access to the deployment's secret stores", Err: err}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	out := map[string]string{}
	for _, g := range grants {
		if g.Jwt != nil && *g.Jwt != "" {
			out[g.StoreId] = *g.Jwt
		}
	}
	return out, nil
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
func (r *resolver) newStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	switch s.Access.Mode {
	case apiclient.StoreAccessModeAgent:
		return r.agentStore(ctx, s, open.namespace)
	case apiclient.StoreAccessModeFederated:
		if open.jwt == "" {
			return nil, deploy.Failf("the control plane granted no access to secret store %s, which is in federated mode", s.Id)
		}
		return r.federatedStore(ctx, s, open)
	}
	return nil, deploy.Failf("secret store %s is opened in %q mode, which this agent cannot use", s.Id, s.Access.Mode)
}

// agentStore opens a store with the agent's own identity, or a credential
// its operator configured.
func (r *resolver) agentStore(ctx context.Context, s apiclient.StoreSettings, namespace string) (secretstore.Store, error) {
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
		return r.vault(s, "kubernetes", func() (string, error) {
			raw, err := os.ReadFile(r.cfg.VaultTokenFile)
			return strings.TrimSpace(string(raw)), err
		})
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

// federatedStore opens a store by exchanging the JWT the control plane
// granted for the store's own credential. The store's configuration decides
// what that credential may read.
func (r *resolver) federatedStore(ctx context.Context, s apiclient.StoreSettings, open opening) (secretstore.Store, error) {
	switch s.Kind {
	case apiclient.StoreSettingsKindAwsSecretsManager:
		if s.Aws == nil {
			return nil, deploy.Failf("secret store %s has no AWS settings", s.Id)
		}
		// The session name is what CloudTrail records the reads under.
		api, err := secretstore.NewAWSWebIdentityClient(ctx, s.Aws.Region, deref(s.Access.RoleArn), "ivaldi-"+open.deploymentID, open.jwt)
		if err != nil {
			return nil, &deploy.Failure{Reason: fmt.Sprintf("secret store %s", s.Id), Err: err}
		}
		return secretstore.NewAWS(api), nil
	case apiclient.StoreSettingsKindVault:
		return r.vault(s, "jwt", func() (string, error) { return open.jwt, nil })
	case apiclient.StoreSettingsKindDoppler:
		if s.Doppler == nil {
			return nil, deploy.Failf("secret store %s has no Doppler settings", s.Id)
		}
		identity := deref(s.Access.IdentityId)
		// Exchanged once, on first use: a retry reads with the same token.
		var token string
		return secretstore.NewDoppler(secretstore.DopplerConfig{
			Project: deref(s.Doppler.Project), Config: deref(s.Doppler.Config),
			Token: func(ctx context.Context) (string, error) {
				if token != "" {
					return token, nil
				}
				t, err := secretstore.DopplerOIDC(ctx, nil, "", identity, open.jwt)
				if err == nil {
					open.mask(t)
					token = t
				}
				return t, err
			},
		}), nil
	}
	return nil, deploy.Failf("this agent cannot open %q secret stores in federated mode", s.Kind)
}

// vault opens a vault store at an address the operator allowed, logging in
// with jwt at the store's auth mount, or defaultMount when it names none.
func (r *resolver) vault(s apiclient.StoreSettings, defaultMount string, jwt func() (string, error)) (secretstore.Store, error) {
	if s.Vault == nil {
		return nil, deploy.Failf("secret store %s has no Vault settings", s.Id)
	}
	if !r.cfg.allows(s.Vault.Address) {
		return nil, deploy.Failf("secret store %s is a Vault at %s, which this agent is not set up to log in to; its operator lists the addresses it may use in the chart's secrets.addresses",
			s.Id, s.Vault.Address)
	}
	mount := deref(s.Access.AuthMount)
	if mount == "" {
		mount = defaultMount
	}
	return secretstore.NewVault(secretstore.VaultConfig{
		Address: s.Vault.Address, Mount: s.Vault.Mount, Namespace: deref(s.Vault.Namespace),
		AuthRole: deref(s.Access.Role), AuthMount: mount, JWT: jwt,
	}), nil
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

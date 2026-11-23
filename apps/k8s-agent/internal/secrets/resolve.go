package secrets

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/secretstore"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// retries is how many times a transient store failure is tried in all.
const retries = 4

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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

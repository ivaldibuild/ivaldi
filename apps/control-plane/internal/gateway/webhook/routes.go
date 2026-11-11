package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

var (
	// ErrRouteNotFound means the control plane knows no active route with
	// that key.
	ErrRouteNotFound = errors.New("webhook route not found")
	// ErrLookupUnavailable means the route lookup could not be completed, so
	// the provider should retry.
	ErrLookupUnavailable = errors.New("webhook route lookup unavailable")
)

// RouteLookup resolves one route key.
type RouteLookup interface {
	Lookup(ctx context.Context, routeKey string) (vcs.WebhookRoute, error)
}

// RouteResolver resolves route keys, from its cache when it can.
type RouteResolver interface {
	Resolve(ctx context.Context, routeKey string) (vcs.WebhookRoute, error)
}

// LookupClient calls the control plane's internal route lookup as the
// gateway's platform workload (ADR 0015).
type LookupClient struct {
	baseURL string
	signer  identity.WorkloadKeySigner
	http    *http.Client
}

// NewLookupClient returns a client for the control plane at baseURL that
// signs each call with the gateway's key. A nil HTTP client gets a short
// default timeout.
func NewLookupClient(baseURL string, signer identity.WorkloadKeySigner, client *http.Client) (*LookupClient, error) {
	switch {
	case baseURL == "":
		return nil, errors.New("webhook lookup: a control plane URL is required")
	case signer == nil:
		return nil, errors.New("webhook lookup: a signer is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &LookupClient{baseURL: baseURL, signer: signer, http: client}, nil
}

func (c *LookupClient) Lookup(ctx context.Context, routeKey string) (vcs.WebhookRoute, error) {
	endpoint := c.baseURL + "/internal/webhook-routes/" + url.PathEscape(routeKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
	}
	assertion, err := c.signer.Sign(identity.AudienceControlPlane)
	if err != nil {
		return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set(identity.CredentialHeader, identity.CredentialHeaderWorkloadKey)

	res, err := c.http.Do(req)
	if err != nil {
		return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		var route vcs.WebhookRoute
		if err := json.NewDecoder(res.Body).Decode(&route); err != nil {
			return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
		}
		return route, nil
	case http.StatusNotFound:
		return vcs.WebhookRoute{}, ErrRouteNotFound
	default:
		return vcs.WebhookRoute{}, fmt.Errorf("%w: the control plane answered %s", ErrLookupUnavailable, res.Status)
	}
}

// Resolver caches route lookups and collapses concurrent misses for the same
// key. Only found routes are cached: a key with no route yet may get one
// moments later, when its Connection is created, and a cached miss would drop
// that installation's webhooks until it expired.
type Resolver struct {
	lookup RouteLookup
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
	group singleflight.Group
}

type cacheEntry struct {
	route   vcs.WebhookRoute
	expires time.Time
}

// NewResolver returns a Resolver over lookup with a positive TTL.
func NewResolver(lookup RouteLookup, ttl time.Duration) (*Resolver, error) {
	switch {
	case lookup == nil:
		return nil, errors.New("webhook resolver: a route lookup is required")
	case ttl <= 0:
		return nil, errors.New("webhook resolver: the cache TTL must be positive")
	}
	return &Resolver{
		lookup: lookup,
		ttl:    ttl,
		now:    clock.Now,
		cache:  map[string]cacheEntry{},
	}, nil
}

func (r *Resolver) Resolve(ctx context.Context, routeKey string) (vcs.WebhookRoute, error) {
	if route, ok := r.cached(routeKey); ok {
		return route, nil
	}
	// The shared lookup must not end when the request that started it does,
	// or every caller waiting on it would fail too. The lookup client's own
	// timeout bounds it; each caller still stops waiting when its request
	// ends.
	results := r.group.DoChan(routeKey, func() (any, error) {
		if route, ok := r.cached(routeKey); ok {
			return route, nil
		}
		route, err := r.lookup.Lookup(context.WithoutCancel(ctx), routeKey)
		if err != nil {
			return nil, err
		}
		r.store(routeKey, route)
		return route, nil
	})
	select {
	case result := <-results:
		if result.Err != nil {
			return vcs.WebhookRoute{}, result.Err
		}
		return result.Val.(vcs.WebhookRoute), nil
	case <-ctx.Done():
		return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, ctx.Err())
	}
}

func (r *Resolver) cached(routeKey string) (vcs.WebhookRoute, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[routeKey]
	if !ok {
		return vcs.WebhookRoute{}, false
	}
	if !r.now().Before(entry.expires) {
		delete(r.cache, routeKey)
		return vcs.WebhookRoute{}, false
	}
	return entry.route, true
}

func (r *Resolver) store(routeKey string, route vcs.WebhookRoute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[routeKey] = cacheEntry{route: route, expires: r.now().Add(r.ttl)}
}

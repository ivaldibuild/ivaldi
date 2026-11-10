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

// LookupClient calls the control plane's internal route lookup.
type LookupClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewLookupClient returns a client for the control plane at baseURL. A nil
// HTTP client gets a short default timeout.
func NewLookupClient(baseURL, token string, client *http.Client) (*LookupClient, error) {
	switch {
	case baseURL == "":
		return nil, errors.New("webhook lookup: a control plane URL is required")
	case token == "":
		return nil, errors.New("webhook lookup: a token is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &LookupClient{baseURL: baseURL, token: token, http: client}, nil
}

func (c *LookupClient) Lookup(ctx context.Context, routeKey string) (vcs.WebhookRoute, error) {
	endpoint := c.baseURL + "/internal/webhook-routes/" + url.PathEscape(routeKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return vcs.WebhookRoute{}, fmt.Errorf("%w: %w", ErrLookupUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Ivaldi-Gateway-Token", c.token)

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
// key.
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
	err     error
	expires time.Time
}

type lookupResult struct {
	route vcs.WebhookRoute
	err   error
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
	if route, err, ok := r.cached(routeKey); ok {
		return route, err
	}
	v, err, _ := r.group.Do(routeKey, func() (any, error) {
		if route, err, ok := r.cached(routeKey); ok {
			return lookupResult{route: route, err: err}, nil
		}
		route, err := r.lookup.Lookup(ctx, routeKey)
		if err != nil && !errors.Is(err, ErrRouteNotFound) {
			return nil, err
		}
		r.store(routeKey, route, err)
		return lookupResult{route: route, err: err}, nil
	})
	if err != nil {
		return vcs.WebhookRoute{}, err
	}
	result := v.(lookupResult)
	return result.route, result.err
}

func (r *Resolver) cached(routeKey string) (vcs.WebhookRoute, error, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[routeKey]
	if !ok {
		return vcs.WebhookRoute{}, nil, false
	}
	if !r.now().Before(entry.expires) {
		delete(r.cache, routeKey)
		return vcs.WebhookRoute{}, nil, false
	}
	return entry.route, entry.err, true
}

func (r *Resolver) store(routeKey string, route vcs.WebhookRoute, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[routeKey] = cacheEntry{route: route, err: err, expires: r.now().Add(r.ttl)}
}

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

var testRoute = vcs.WebhookRoute{
	OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
	ConnectionID:   "vcn_01ARZ3NDEKTSV4RRFFQ69G5FAV",
	Provider:       vcs.ProviderGitHub,
	HostURL:        "https://github.com",
}

type fakeRouteLookup struct {
	calls int
	route vcs.WebhookRoute
	err   error
}

func (f *fakeRouteLookup) Lookup(context.Context, string) (vcs.WebhookRoute, error) {
	f.calls++
	return f.route, f.err
}

func TestResolverCachesAHit(t *testing.T) {
	lookup := &fakeRouteLookup{route: testRoute}
	resolver, err := NewResolver(lookup, time.Minute)
	require.NoError(t, err)

	first, err := resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	second, err := resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, lookup.calls)
}

func TestResolverDoesNotCacheNotFound(t *testing.T) {
	lookup := &fakeRouteLookup{err: ErrRouteNotFound}
	resolver, err := NewResolver(lookup, time.Minute)
	require.NoError(t, err)

	_, err = resolver.Resolve(context.Background(), "github:github.com:42")
	assert.ErrorIs(t, err, ErrRouteNotFound)

	// The Connection is created; the next webhook must find it.
	lookup.err = nil
	lookup.route = testRoute
	route, err := resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	assert.Equal(t, testRoute, route)
	assert.Equal(t, 2, lookup.calls)
}

func TestResolverLookupOutlivesTheCallerThatStartedIt(t *testing.T) {
	lookup := &ctxRouteLookup{}
	resolver, err := NewResolver(lookup, time.Minute)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The caller has gone, so it may or may not see the answer; the lookup it
	// started must still complete for the next caller.
	_, _ = resolver.Resolve(ctx, "github:github.com:42")

	route, err := resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	assert.Equal(t, testRoute, route)
	assert.NoError(t, lookup.lastErr, "the lookup ran with a live context")
}

// ctxRouteLookup answers testRoute and records whether the context it got was
// already cancelled.
type ctxRouteLookup struct {
	lastErr error
}

func (f *ctxRouteLookup) Lookup(ctx context.Context, _ string) (vcs.WebhookRoute, error) {
	f.lastErr = ctx.Err()
	if f.lastErr != nil {
		return vcs.WebhookRoute{}, f.lastErr
	}
	return testRoute, nil
}

func TestResolverDoesNotCacheAnUnavailableLookup(t *testing.T) {
	lookup := &fakeRouteLookup{err: ErrLookupUnavailable}
	resolver, err := NewResolver(lookup, time.Minute)
	require.NoError(t, err)

	_, _ = resolver.Resolve(context.Background(), "github:github.com:42")
	_, _ = resolver.Resolve(context.Background(), "github:github.com:42")
	assert.Equal(t, 2, lookup.calls)
}

func TestResolverExpiresTheCache(t *testing.T) {
	lookup := &fakeRouteLookup{route: testRoute}
	resolver, err := NewResolver(lookup, time.Minute)
	require.NoError(t, err)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	resolver.now = func() time.Time { return now }

	_, err = resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	_, err = resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	assert.Equal(t, 1, lookup.calls)

	now = now.Add(2 * time.Minute)
	_, err = resolver.Resolve(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	assert.Equal(t, 2, lookup.calls)
}

func TestNewResolverValidatesItsInput(t *testing.T) {
	_, err := NewResolver(nil, time.Minute)
	assert.Error(t, err)
	_, err = NewResolver(&fakeRouteLookup{}, 0)
	assert.Error(t, err)
}

func TestLookupClientCallsTheInternalEndpoint(t *testing.T) {
	var gotPath, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Ivaldi-Gateway-Token")
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(testRoute))
	}))
	defer server.Close()

	client, err := NewLookupClient(server.URL, "lookup-token", server.Client())
	require.NoError(t, err)
	route, err := client.Lookup(context.Background(), "github:github.com:42")
	require.NoError(t, err)
	assert.Equal(t, testRoute, route)
	assert.Equal(t, "/internal/webhook-routes/github:github.com:42", gotPath)
	assert.Equal(t, "lookup-token", gotToken)
}

func TestLookupClientMapsTheControlPlaneAnswers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"not found", http.StatusNotFound, ErrRouteNotFound},
		{"unavailable", http.StatusServiceUnavailable, ErrLookupUnavailable},
		{"unauthorized", http.StatusUnauthorized, ErrLookupUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			client, err := NewLookupClient(server.URL, "token", server.Client())
			require.NoError(t, err)
			_, err = client.Lookup(context.Background(), "github:github.com:42")
			assert.True(t, errors.Is(err, tt.want), "got %v", err)
		})
	}
}

func TestLookupClientRejectsBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()
	client, err := NewLookupClient(server.URL, "token", server.Client())
	require.NoError(t, err)
	_, err = client.Lookup(context.Background(), "github:github.com:42")
	assert.ErrorIs(t, err, ErrLookupUnavailable)
}

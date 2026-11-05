package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

type pingOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// fakeRoutes registers a public operation, a secured one, and one that panics.
type fakeRoutes struct{}

func (fakeRoutes) RegisterRoutes(api huma.API) {
	huma.Get(api, "/ping", func(context.Context, *struct{}) (*pingOutput, error) {
		out := &pingOutput{}
		out.Body.Message = "pong"
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "secret",
		Method:      http.MethodGet,
		Path:        "/secret",
		Security:    []map[string][]string{{BearerAuth: {}}},
	}, func(ctx context.Context, _ *struct{}) (*pingOutput, error) {
		out := &pingOutput{}
		if c, ok := identity.CallerFrom(ctx); ok {
			out.Body.Message = c.Actor.PrincipalID + "@" + c.OrganizationID
		}
		return out, nil
	})
	huma.Get(api, "/boom", func(context.Context, *struct{}) (*pingOutput, error) {
		panic("boom")
	})
}

// logged matches a log message with n fields of any value.
func logged(msg string, n int) []any {
	args := []any{msg}
	for range n {
		args = append(args, mock.Anything)
	}
	return args
}

func testConfig() *config.Config {
	port := 0
	return &config.Config{Host: "127.0.0.1", Port: &port, ShutdownTimeout: time.Second}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(testConfig(), "test", logging.NewMockLogger(), WithRoutes(fakeRoutes{}))
	require.NoError(t, err)
	return s
}

func serve(s *Server, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServerMountsRoutesUnderBasePath(t *testing.T) {
	s := newTestServer(t)

	rec := serve(s, http.MethodGet, "/v1/ping")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"message":"pong"`)

	assert.Equal(t, http.StatusNotFound, serve(s, http.MethodGet, "/ping").Code, "operations are only served under the base path")
}

func TestServerPublishesTheOpenAPIDocument(t *testing.T) {
	rec := serve(newTestServer(t), http.MethodGet, "/openapi.json")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"/v1/ping"`)
}

func TestServerHealth(t *testing.T) {
	assert.Equal(t, http.StatusNoContent, serve(newTestServer(t), http.MethodGet, "/healthz").Code)
}

func TestServerRefusesSecuredOperationsWithoutAVerifier(t *testing.T) {
	rec := serve(newTestServer(t), http.MethodGet, "/v1/secret")

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "with nothing to check tokens, secured operations fail closed")
}

// fakeVerifier accepts "good" and "expired" as tokens, and breaks on "broken".
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	switch token {
	case "good":
		return &identity.Claims{Subject: "prn_ada", OrganizationID: "org_acme", Kind: identity.ActorUser}, nil
	case "expired":
		return nil, identity.ErrTokenExpired
	case "broken":
		return nil, errors.New("key source down")
	}
	return nil, identity.ErrInvalidToken
}

func TestServerAuthenticatesBearerTokens(t *testing.T) {
	logger := logging.NewMockLogger()
	logger.On("Error", logged("verify access token", 1)...).Maybe()
	cfg := testConfig()
	cfg.ExternalURL = "https://ivaldi.example.com/"
	s, err := New(cfg, "test", logger, WithRoutes(fakeRoutes{}), WithVerifier(fakeVerifier{}))
	require.NoError(t, err)
	call := func(authorization string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/secret", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	const metadata = `resource_metadata="https://ivaldi.example.com/.well-known/oauth-protected-resource"`

	t.Run("a valid token reaches the handler with its caller", func(t *testing.T) {
		rec := call("Bearer good")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"message":"prn_ada@org_acme"`)
	})
	t.Run("the scheme is case-insensitive", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, call("bearer good").Code)
	})
	t.Run("no token", func(t *testing.T) {
		rec := call("")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, "Bearer "+metadata, rec.Header().Get("WWW-Authenticate"), "tells the client where to get a token")
	})
	t.Run("another scheme", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, call("Basic YWRhOnB3").Code)
	})
	t.Run("invalid token", func(t *testing.T) {
		rec := call("Bearer forged")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	})
	t.Run("expired token", func(t *testing.T) {
		rec := call("Bearer expired")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "expired")
	})
	t.Run("verification fails", func(t *testing.T) {
		assert.Equal(t, http.StatusInternalServerError, call("Bearer broken").Code)
	})
	t.Run("public operations need no token", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, serve(s, http.MethodGet, "/v1/ping").Code)
	})
	t.Run("protected resource metadata", func(t *testing.T) {
		rec := serve(s, http.MethodGet, "/.well-known/oauth-protected-resource")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"resource":"https://ivaldi.example.com","authorization_servers":["https://ivaldi.example.com"],"bearer_methods_supported":["header"]}`, rec.Body.String())
	})
	t.Run("the OpenAPI document declares the bearer scheme", func(t *testing.T) {
		assert.Contains(t, serve(s, http.MethodGet, "/openapi.json").Body.String(), `"bearerFormat":"JWT"`)
	})
}

type fakeHandlers struct{}

func (fakeHandlers) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/hello", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) })
}

func TestServerMountsPlainHandlers(t *testing.T) {
	s, err := New(testConfig(), "test", logging.NewMockLogger(), WithHandlers(fakeHandlers{}))
	require.NoError(t, err)

	rec := serve(s, http.MethodGet, "/auth/hello")

	assert.Equal(t, "hi", rec.Body.String())
}

func TestServerRecoversPanics(t *testing.T) {
	logger := logging.NewMockLogger()
	logger.On("Error", logged("panic serving request", 4)...).Once()
	s, err := New(testConfig(), "test", logger, WithRoutes(fakeRoutes{}))
	require.NoError(t, err)

	rec := serve(s, http.MethodGet, "/v1/boom")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	logger.AssertExpectations(t)
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	t.Run("no port", func(t *testing.T) {
		cfg := testConfig()
		cfg.Port = nil
		_, err := New(cfg, "test", logging.NewMockLogger())
		require.ErrorContains(t, err, "port is required")
	})
	t.Run("certificate without key", func(t *testing.T) {
		cfg := testConfig()
		cfg.TLS.CertFile = "cert.pem"
		_, err := New(cfg, "test", logging.NewMockLogger())
		require.ErrorContains(t, err, "both a certificate and a key")
	})
}

func TestServerRunStopsWhenTheContextIsDone(t *testing.T) {
	logger := logging.NewMockLogger()
	logger.On("Info", logged("api listening", 1)...)
	s, err := New(testConfig(), "test", logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}

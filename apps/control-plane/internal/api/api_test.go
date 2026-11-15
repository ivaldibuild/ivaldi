package api

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	}, public)
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
	}, public)
}

func public(op *huma.Operation) { op.Metadata = map[string]any{Public: true} }

// openRoutes registers an operation that neither requires a token nor is
// marked public.
type openRoutes struct{}

func (openRoutes) RegisterRoutes(api huma.API) {
	huma.Get(api, "/forgotten", func(context.Context, *struct{}) (*pingOutput, error) {
		return &pingOutput{}, nil
	})
}

func TestNewRefusesOperationsThatAreNeitherSecuredNorPublic(t *testing.T) {
	_, err := New(testConfig(), "test", logging.NewMockLogger(), WithRoutes(fakeRoutes{}, openRoutes{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GET /v1/forgotten")
	assert.NotContains(t, err.Error(), "ping")
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

// The web app answers what the API does not, and never what it does: a
// regression here would turn an API 404 into an HTML page that a client reads
// as a success.
func TestServerServesTheWebAppAfterTheAPI(t *testing.T) {
	cfg := testConfig()
	web := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html>"))
	})
	s, err := New(cfg, "test", logging.NewMockLogger(), WithRoutes(fakeRoutes{}), WithWebUI(web))
	require.NoError(t, err)

	send := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	call := func(path string) *httptest.ResponseRecorder { return send(http.MethodGet, path) }

	app := call("/clusters/eng")
	assert.Equal(t, http.StatusOK, app.Code)
	assert.Contains(t, app.Body.String(), "<!doctype html>", "a route of the single-page app")

	api := call("/v1/ping")
	assert.Equal(t, http.StatusOK, api.Code)
	assert.Contains(t, api.Body.String(), "pong", "the API keeps its paths")

	unknown := call("/v1/nope")
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Contains(t, unknown.Header().Get("Content-Type"), "application/problem+json")
	assert.NotContains(t, unknown.Body.String(), "<!doctype html>", "an unknown API path is not the web app")

	unknownPost := send(http.MethodPost, "/v1/nope")
	assert.Equal(t, http.StatusNotFound, unknownPost.Code)
	assert.Contains(t, unknownPost.Header().Get("Content-Type"), "application/problem+json", "every method gets the API's errors")

	wrongMethod := send(http.MethodPost, "/v1/ping")
	assert.Equal(t, http.StatusMethodNotAllowed, wrongMethod.Code, "an operation under another method is a 405, not a 404")
	assert.Equal(t, http.MethodGet, wrongMethod.Header().Get("Allow"))
	assert.Contains(t, wrongMethod.Header().Get("Content-Type"), "application/problem+json")

	health := call("/healthz")
	assert.Equal(t, http.StatusNoContent, health.Code)
}

// failingRoutes registers an operation that fails unexpectedly.
type failingRoutes struct{}

func (failingRoutes) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "fail", Method: http.MethodGet, Path: "/fail", Metadata: map[string]any{Public: true},
	}, func(ctx context.Context, _ *struct{}) (*pingOutput, error) {
		return nil, InternalError(ctx, errors.New(`pq: relation "secret_table" does not exist`))
	})
}

func TestInternalErrorLogsAndHidesTheCause(t *testing.T) {
	logger := logging.NewMockLogger()
	logger.On("Error", logged("request failed", 4)...).Once()
	s, err := New(testConfig(), "test", logger, WithRoutes(failingRoutes{}))
	require.NoError(t, err)

	rec := serve(s, http.MethodGet, "/v1/fail")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "secret_table")
	logger.AssertExpectations(t)
}

type recordingVerifier struct{ credential string }

func (v *recordingVerifier) Verify(_ context.Context, credential string) (*identity.Claims, error) {
	v.credential = credential
	return &identity.Claims{Subject: "prn_agent", OrganizationID: "org_acme", Kind: identity.ActorWorkload}, nil
}

func TestServerVerifiesWorkloadCredentials(t *testing.T) {
	expiry := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	cert := &x509.Certificate{Raw: []byte("certificate"), NotAfter: expiry}
	sum := sha256.Sum256(cert.Raw)

	v := &recordingVerifier{}
	s, err := New(testConfig(), "test", logging.NewMockLogger(), WithRoutes(fakeRoutes{}), WithVerifier(v))
	require.NoError(t, err)
	call := func(withCert, withBearer, kubernetes bool) {
		req := httptest.NewRequest(http.MethodGet, "/v1/secret", nil)
		if withCert {
			req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
		}
		if withBearer {
			req.Header.Set("Authorization", "Bearer projected")
		}
		if kubernetes {
			req.Header.Set("X-Ivaldi-Credential", "kubernetes")
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	call(true, false, false)
	assert.Equal(t, identity.MTLSCredential(hex.EncodeToString(sum[:]), expiry), v.credential)

	call(false, true, true)
	assert.Equal(t, identity.KubernetesCredential("projected"), v.credential)

	call(true, true, false)
	assert.Equal(t, "projected", v.credential, "an explicit bearer token wins over a client certificate")

	req := httptest.NewRequest(http.MethodGet, "/v1/secret", nil)
	req.Header.Set("Authorization", "Bearer signed-assertion")
	req.Header.Set(identity.CredentialHeader, identity.CredentialHeaderWorkloadKey)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, identity.WorkloadKeyCredential("signed-assertion"), v.credential)
}

func TestServerRefusesBearerTokensWithInternalCredentialPrefixes(t *testing.T) {
	v := &recordingVerifier{}
	s, err := New(testConfig(), "test", logging.NewMockLogger(), WithRoutes(fakeRoutes{}), WithVerifier(v))
	require.NoError(t, err)
	forged := map[string]string{
		"mtls": identity.MTLSCredential("0011aabb", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)),
		"k8s":  identity.KubernetesCredential("projected"),
		"wkey": identity.WorkloadKeyCredential("signed-assertion"),
	}
	for name, token := range forged {
		t.Run(name, func(t *testing.T) {
			v.credential = ""
			req := httptest.NewRequest(http.MethodGet, "/v1/secret", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
			assert.Empty(t, v.credential, "the forged credential never reaches the verifier")
		})
	}
}

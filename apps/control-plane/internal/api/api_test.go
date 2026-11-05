package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
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
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(context.Context, *struct{}) (*pingOutput, error) {
		return &pingOutput{}, nil
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
	s, err := New(testConfig(), "test", logging.NewMockLogger(), fakeRoutes{})
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

func TestServerRefusesSecuredOperations(t *testing.T) {
	rec := serve(newTestServer(t), http.MethodGet, "/v1/secret")

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "nothing authenticates callers yet, so secured operations fail closed")
}

func TestServerRecoversPanics(t *testing.T) {
	logger := logging.NewMockLogger()
	logger.On("Error", logged("panic serving request", 4)...).Once()
	s, err := New(testConfig(), "test", logger, fakeRoutes{})
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

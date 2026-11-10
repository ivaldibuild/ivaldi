package webhookroutes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const testToken = "lookup-token"

type fakeRepo struct {
	connection *versioncontrol.Connection
	err        error
}

func (f fakeRepo) CreateConnection(context.Context, *versioncontrol.Connection) error { return nil }
func (f fakeRepo) GetConnection(context.Context, string, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f fakeRepo) GetConnectionForUpdate(context.Context, string, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f fakeRepo) GetConnectionByWebhookRouteKey(context.Context, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f fakeRepo) GetConnectionByWebhookRouteKeyForUpdate(context.Context, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f fakeRepo) ListConnections(context.Context, string, bool) ([]*versioncontrol.Connection, error) {
	return nil, nil
}
func (f fakeRepo) UpdateConnection(context.Context, *versioncontrol.Connection) error { return nil }

func testConnection(t *testing.T) *versioncontrol.Connection {
	t.Helper()
	connection, err := versioncontrol.NewInstalledConnection(
		"org_01ARZ3NDEKTSV4RRFFQ69G5FAV", nil,
		vcs.Installation{
			Provider: vcs.ProviderGitHub, HostURL: "https://github.com",
			InstallationID: "42", AccountName: "acme",
		},
	)
	require.NoError(t, err)
	return connection
}

func serve(t *testing.T, connection *versioncontrol.Connection, err error, token string) *httptest.ResponseRecorder {
	t.Helper()
	controller, errNew := New(fakeRepo{connection: connection, err: err}, testToken, logging.Nop())
	require.NoError(t, errNew)
	mux := http.NewServeMux()
	controller.RegisterHandlers(mux)
	req := httptest.NewRequest(http.MethodGet, "/internal/webhook-routes/github:github.com:42", nil)
	if token != "" {
		req.Header.Set(TokenHeader, token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestKnownRouteReturnsTheConnection(t *testing.T) {
	rec := serve(t, testConnection(t), nil, testToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var route vcs.WebhookRoute
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &route))
	assert.Equal(t, "org_01ARZ3NDEKTSV4RRFFQ69G5FAV", route.OrganizationID)
	assert.Equal(t, vcs.ProviderGitHub, route.Provider)
	assert.Equal(t, "https://github.com", route.HostURL)
	assert.NotEmpty(t, route.ConnectionID)
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	rec := serve(t, nil, versioncontrol.ErrNotFound, testToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLookupFailureReturnsInternalServerError(t *testing.T) {
	rec := serve(t, nil, errors.New("database unavailable"), testToken)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestASuspendedConnectionStillResolves(t *testing.T) {
	connection := testConnection(t)
	require.NoError(t, connection.Suspend())
	rec := serve(t, connection, nil, testToken)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMissingOrWrongTokenIsRefused(t *testing.T) {
	for name, token := range map[string]string{"missing": "", "wrong": "other"} {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, testConnection(t), nil, token)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestNewRequiresADependency(t *testing.T) {
	_, err := New(nil, testToken, logging.Nop())
	assert.Error(t, err)
	_, err = New(fakeRepo{}, "", logging.Nop())
	assert.Error(t, err)
	_, err = New(fakeRepo{}, testToken, nil)
	assert.Error(t, err)
}

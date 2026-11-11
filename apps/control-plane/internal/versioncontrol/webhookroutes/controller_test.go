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

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const testAssertion = "signed-assertion"

// fakeVerifier accepts testAssertion, sent as a workload-key credential, as
// the gateway.
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, credential string) (*identity.Claims, error) {
	if credential != identity.WorkloadKeyCredential(testAssertion) {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: "prn_gateway", Kind: identity.ActorWorkload}, nil
}

// fakeAuthorizer records the request and answers with outcome.
type fakeAuthorizer struct {
	outcome authorization.Outcome
	asked   *authorization.Request
}

func (f fakeAuthorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	*f.asked = req
	return authorization.Decision{ID: "dcn_test", Outcome: f.outcome}, nil
}

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

type call struct {
	connection *versioncontrol.Connection
	err        error
	assertion  string
	kind       string
	outcome    authorization.Outcome
}

func serve(t *testing.T, c call) (*httptest.ResponseRecorder, authorization.Request) {
	t.Helper()
	if c.outcome == "" {
		c.outcome = authorization.OutcomeAllow
	}
	var asked authorization.Request
	controller, errNew := New(fakeRepo{connection: c.connection, err: c.err}, fakeVerifier{},
		fakeAuthorizer{outcome: c.outcome, asked: &asked}, logging.Nop())
	require.NoError(t, errNew)
	mux := http.NewServeMux()
	controller.RegisterHandlers(mux)
	req := httptest.NewRequest(http.MethodGet, "/internal/webhook-routes/github:github.com:42", nil)
	if c.assertion != "" {
		req.Header.Set("Authorization", "Bearer "+c.assertion)
	}
	if c.kind != "" {
		req.Header.Set(identity.CredentialHeader, c.kind)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec, asked
}

// gateway is a call the gateway makes, with its signed assertion.
func gateway(connection *versioncontrol.Connection, err error) call {
	return call{connection: connection, err: err, assertion: testAssertion, kind: identity.CredentialHeaderWorkloadKey}
}

func TestKnownRouteReturnsTheConnection(t *testing.T) {
	rec, asked := serve(t, gateway(testConnection(t), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, authorization.Request{
		Actor:      identity.Actor{PrincipalID: "prn_gateway", Kind: identity.ActorWorkload},
		Permission: versioncontrol.PermissionWebhookRouteResolve,
		ResourceID: authorization.PlatformResource,
		Action:     versioncontrol.ActionWebhookRouteResolve,
	}, asked, "it asks the authorizer as a platform request")
	var route vcs.WebhookRoute
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &route))
	assert.Equal(t, "org_01ARZ3NDEKTSV4RRFFQ69G5FAV", route.OrganizationID)
	assert.Equal(t, vcs.ProviderGitHub, route.Provider)
	assert.Equal(t, "https://github.com", route.HostURL)
	assert.NotEmpty(t, route.ConnectionID)
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	rec, _ := serve(t, gateway(nil, versioncontrol.ErrNotFound))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLookupFailureReturnsInternalServerError(t *testing.T) {
	rec, _ := serve(t, gateway(nil, errors.New("database unavailable")))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestASuspendedConnectionStillResolves(t *testing.T) {
	connection := testConnection(t)
	require.NoError(t, connection.Suspend())
	rec, _ := serve(t, gateway(connection, nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestACallerWithoutAValidAssertionIsRefused(t *testing.T) {
	tests := map[string]call{
		"no credential":           {},
		"an assertion that fails": {assertion: "forged", kind: identity.CredentialHeaderWorkloadKey},
		"an access token":         {assertion: testAssertion},
		"an internal encoding":    {assertion: identity.WorkloadKeyCredential(testAssertion), kind: identity.CredentialHeaderWorkloadKey},
		"a kubernetes token":      {assertion: testAssertion, kind: identity.CredentialHeaderKubernetes},
	}
	for name, c := range tests {
		t.Run(name, func(t *testing.T) {
			c.connection = testConnection(t)
			rec, _ := serve(t, c)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestACallerTheAuthorizerDeniesIsForbidden(t *testing.T) {
	c := gateway(testConnection(t), nil)
	c.outcome = authorization.OutcomeDeny
	rec, _ := serve(t, c)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTheGatewayHoldsThePermission(t *testing.T) {
	assert.True(t, iam.PlatformComponentAllows(iam.ComponentGateway, versioncontrol.PermissionWebhookRouteResolve),
		"IAM's platform permissions name this capability's permission")
}

func TestNewRequiresADependency(t *testing.T) {
	var asked authorization.Request
	authz := fakeAuthorizer{outcome: authorization.OutcomeAllow, asked: &asked}
	_, err := New(nil, fakeVerifier{}, authz, logging.Nop())
	assert.Error(t, err)
	_, err = New(fakeRepo{}, nil, authz, logging.Nop())
	assert.Error(t, err)
	_, err = New(fakeRepo{}, fakeVerifier{}, nil, logging.Nop())
	assert.Error(t, err)
	_, err = New(fakeRepo{}, fakeVerifier{}, authz, nil)
	assert.Error(t, err)
}

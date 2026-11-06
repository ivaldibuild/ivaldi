package workspacetest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// Token is the access token Server accepts, for Caller in Organization.
const Token = "ada"

// Caller is the actor Token authenticates.
var Caller = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

// Organization is the Organization Token belongs to.
const Organization = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"

type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != Token {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: Caller.PrincipalID, Kind: Caller.Kind, OrganizationID: Organization}, nil
}

// Server serves routes the way the control plane does, accepting Token.
type Server struct {
	server *api.Server
}

// NewServer returns a Server for routes.
func NewServer(t testing.TB, routes ...api.Routes) *Server {
	t.Helper()
	port := 0
	s, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(routes...), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	return &Server{server: s}
}

// Call sends a request to path, below api.BasePath, with token if it is
// set, and returns the response and its decoded JSON body.
func (s *Server) Call(t testing.TB, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, api.BasePath+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.server.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// Data returns a response body's data object.
func Data(t testing.TB, body map[string]any) map[string]any {
	t.Helper()
	d, ok := body["data"].(map[string]any)
	require.True(t, ok, "the body has a data object: %v", body)
	return d
}

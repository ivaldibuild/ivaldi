package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// verifier accepts the token "ada", for Ada in the world's Organization.
type verifier struct{ w *world }

func (v verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: v.w.ada, Kind: identity.ActorUser, OrganizationID: v.w.org.ID()}, nil
}

type httpWorld struct {
	*world
	server *api.Server
	logger *logging.MockLogger
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	w := newWorld(t)
	port := 0
	logger := logging.NewMockLogger()
	s, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logger,
		api.WithRoutes(NewProfileController(w.svc)), api.WithVerifier(verifier{w}))
	require.NoError(t, err)
	return &httpWorld{world: w, server: s, logger: logger}
}

func (h *httpWorld) get(t *testing.T, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, api.BasePath+"/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestGetMeNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	rec, _ := h.get(t, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec, _ = h.get(t, "forged")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetMe(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.get(t, "ada")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Status string `json:"status"`
		Data   Me     `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "success", body.Status)
	assert.Equal(t, MePrincipal{ID: h.ada, Kind: iam.PrincipalUser, DisplayName: "Ada", Email: new("ada@example.com")}, body.Data.Principal)
	assert.Equal(t, MeOrganization{ID: h.org.ID(), Slug: "acme", DisplayName: "Acme"}, body.Data.Organization)
	assert.Equal(t, []MeRole{
		{Role: "owner", RoleID: role(iam.RoleOwner), ResourceID: h.org.ID()},
		{Role: "member", RoleID: role(iam.RoleMember), ResourceID: h.org.ID()},
	}, body.Data.Roles)

	// The JSON keys are the web app's contract.
	data := out["data"].(map[string]any)
	assert.ElementsMatch(t, []string{"principal", "organization", "roles"}, keys(data))
	assert.ElementsMatch(t, []string{"id", "kind", "display_name", "email"}, keys(data["principal"].(map[string]any)))
	assert.ElementsMatch(t, []string{"id", "slug", "display_name", "logo_url"}, keys(data["organization"].(map[string]any)))
	assert.ElementsMatch(t, []string{"role", "role_id", "resource_id"}, keys(data["roles"].([]any)[0].(map[string]any)))
}

func TestGetMeRefusesAnInactiveCaller(t *testing.T) {
	h := newHTTPWorld(t)
	h.access.PrincipalStatus = iam.PrincipalSuspended
	rec, _ := h.get(t, "ada")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	h.access.PrincipalStatus = ""
	rec, _ = h.get(t, "ada")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGetMeHidesUnexpectedErrors(t *testing.T) {
	h := newHTTPWorld(t)
	h.accessErr = errors.New(`pq: relation "iam.grants" does not exist`)
	logged := mock.MatchedBy(func(f logging.Field) bool {
		return f.Key == "error" && strings.Contains(fmt.Sprint(f.Value), "iam.grants")
	})
	h.logger.On("Error", "request failed", mock.Anything, mock.Anything, mock.Anything, logged).Once()

	rec, _ := h.get(t, "ada")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "iam.grants")
	h.logger.AssertExpectations(t)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

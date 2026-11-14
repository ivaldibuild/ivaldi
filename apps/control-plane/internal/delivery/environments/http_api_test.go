package environments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// verifier accepts the token "ada", for Ada in the test Organization.
type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada.PrincipalID, Kind: ada.Kind, OrganizationID: org}, nil
}

type httpWorld struct {
	*world
	server *api.Server
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	w := newWorld(t)
	port := 0
	s, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewEnvironmentController(w.svc)), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	return &httpWorld{world: w, server: s}
}

func (h *httpWorld) call(t *testing.T, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, api.BasePath+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/workspaces/" + wsActive + "/environments"},
		{http.MethodGet, "/workspaces/" + wsActive + "/environments"},
		{http.MethodGet, "/environments/env_x"},
		{http.MethodPatch, "/environments/env_x"},
		{http.MethodPost, "/environments/env_x/archive"},
		{http.MethodPost, "/environments/env_x/restore"},
		{http.MethodPost, "/environments/env_x/targets"},
		{http.MethodGet, "/environments/env_x/targets"},
		{http.MethodGet, "/targets/tgt_x"},
		{http.MethodPatch, "/targets/tgt_x"},
		{http.MethodPost, "/targets/tgt_x/archive"},
		{http.MethodPost, "/targets/tgt_x/restore"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.asked)
}

func TestCreateEnvironmentOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodPost, "/workspaces/"+wsActive+"/environments", "ada",
		`{"display_name": "Production", "tier": "production", "execution_enabled": true}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	data := out["data"].(map[string]any)
	assert.Equal(t, "production", data["slug"])
	assert.Equal(t, wsActive, data["workspace_id"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	require.Len(t, h.authz.asked, 1)
	assert.Equal(t, ada, h.authz.asked[0].Actor)
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *httpWorld) (method, path, body string)
		want         int
		wantNotAsked bool
	}{
		{"an unknown tier is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsActive + "/environments", `{"display_name": "Production", "tier": "moon", "execution_enabled": true}`
		}, http.StatusUnprocessableEntity, true},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			return http.MethodPost, "/workspaces/" + wsActive + "/environments", `{"display_name": "Production", "tier": "production", "execution_enabled": true}`
		}, http.StatusForbidden, false},
		{"a missing environment", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/environments/env_01JZ0000000000000000000099/restore", ""
		}, http.StatusNotFound, false},
		{"a missing target", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/targets/tgt_01JZ0000000000000000000099", ""
		}, http.StatusNotFound, false},
		{"archiving an environment in use", func(h *httpWorld) (string, string, string) {
			env := h.env(t, wsActive, "staging", false)
			h.target(t, env.ID(), "web", false)
			return http.MethodPost, "/environments/" + env.ID() + "/archive", ""
		}, http.StatusConflict, false},
		{"a taken slug", func(h *httpWorld) (string, string, string) {
			h.env(t, wsActive, "production", false)
			return http.MethodPost, "/workspaces/" + wsActive + "/environments", `{"display_name": "Production", "tier": "production", "execution_enabled": true}`
		}, http.StatusConflict, false},
		{"a domain rule", func(h *httpWorld) (string, string, string) {
			env := h.env(t, wsActive, "staging", false)
			return http.MethodPost, "/environments/" + env.ID() + "/targets", `{"display_name": "Web", "runtime": "kubernetes", "locator": {}}`
		}, http.StatusUnprocessableEntity, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(h)
			rec, _ := h.call(t, method, path, "ada", body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.asked)
			}
		})
	}
}

func TestTargetsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	env := h.env(t, wsActive, "staging", false)

	rec, out := h.call(t, http.MethodPost, "/environments/"+env.ID()+"/targets", "ada",
		`{"display_name": "Web", "runtime": "kubernetes", "locator": {"cluster": "prod-1", "namespace": "web"}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	id := out["data"].(map[string]any)["id"].(string)

	rec, out = h.call(t, http.MethodGet, "/environments/"+env.ID()+"/targets", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	items := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, id, items[0].(map[string]any)["id"])

	rec, out = h.call(t, http.MethodPost, "/targets/"+id+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, out["data"].(map[string]any)["deleted_at"])

	assert.Equal(t, []string{delivery.EventTargetCreated, delivery.EventTargetArchived}, h.auditTypes())
}

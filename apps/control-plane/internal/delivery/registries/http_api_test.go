package registries

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
		api.WithRoutes(NewRegistryController(w.svc)), api.WithVerifier(verifier{}))
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
		{http.MethodPost, "/workspaces/" + wsActive + "/registries"},
		{http.MethodGet, "/workspaces/" + wsActive + "/registries"},
		{http.MethodGet, "/registries/reg_x"},
		{http.MethodPatch, "/registries/reg_x"},
		{http.MethodPut, "/registries/reg_x/credential"},
		{http.MethodPut, "/registries/reg_x/lookup"},
		{http.MethodPost, "/registries/reg_x/archive"},
		{http.MethodPost, "/registries/reg_x/restore"},
	} {
		for _, token := range []string{"", "bogus"} {
			rec, _ := h.call(t, op.method, op.path, token, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with token %q", op.method, op.path, token)
		}
	}
	assert.Empty(t, h.authz.asked)
}

func TestRegistriesOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodPost, "/workspaces/"+wsActive+"/registries", "ada",
		`{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io", "credential_ref": "crd_1"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	data := out["data"].(map[string]any)
	id := data["id"].(string)
	assert.Equal(t, "github", data["slug"])
	assert.Equal(t, wsActive, data["workspace_id"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	assert.Equal(t, "crd_1", data["credential_ref"])
	assert.Equal(t, "control_plane", data["lookup_from"])

	rec, out = h.call(t, http.MethodPatch, "/registries/"+id, "ada", `{"url": "https://ghcr.example.com"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "https://ghcr.example.com", out["data"].(map[string]any)["url"])

	rec, out = h.call(t, http.MethodPut, "/registries/"+id+"/credential", "ada", `{"credential_ref": ""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, out["data"].(map[string]any)["credential_ref"])

	rec, out = h.call(t, http.MethodPut, "/registries/"+id+"/lookup", "ada", `{"lookup_from": "data_plane", "lookup_pool": "pool-a"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "pool-a", out["data"].(map[string]any)["lookup_pool"])

	rec, out = h.call(t, http.MethodGet, "/workspaces/"+wsActive+"/registries", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	items := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, id, items[0].(map[string]any)["id"])

	rec, out = h.call(t, http.MethodPost, "/registries/"+id+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, out["data"].(map[string]any)["deleted_at"])

	rec, out = h.call(t, http.MethodPost, "/registries/"+id+"/restore", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, out["data"].(map[string]any)["deleted_at"])

	rec, _ = h.call(t, http.MethodGet, "/registries/"+id, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{
		delivery.EventRegistryCreated, delivery.EventRegistryUpdated, delivery.EventRegistryUpdated, delivery.EventRegistryUpdated,
		delivery.EventRegistryArchived, delivery.EventRegistryRestored,
	}, h.auditTypes())
	for _, req := range h.authz.asked {
		assert.Equal(t, ada, req.Actor)
	}
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *httpWorld) (method, path, body string)
		want         int
		wantNotAsked bool
	}{
		{"an unknown kind is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsActive + "/registries", `{"display_name": "GitHub", "kind": "npm", "url": "https://ghcr.io"}`
		}, http.StatusUnprocessableEntity, true},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			return http.MethodPost, "/workspaces/" + wsActive + "/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io"}`
		}, http.StatusForbidden, false},
		{"needs approval", func(h *httpWorld) (string, string, string) {
			h.authz.outcome = authorization.OutcomeRequiresApproval
			return http.MethodPost, "/workspaces/" + wsActive + "/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io"}`
		}, http.StatusForbidden, false},
		{"a missing registry", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/registries/reg_01JZ0000000000000000000099", ""
		}, http.StatusNotFound, true},
		{"a missing workspace", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/wsp_01JZ0000000000000000000099/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io"}`
		}, http.StatusNotFound, false},
		{"an archived workspace", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsArchived + "/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io"}`
		}, http.StatusConflict, false},
		{"a taken slug", func(h *httpWorld) (string, string, string) {
			h.registry(t, wsActive, "github", false)
			return http.MethodPost, "/workspaces/" + wsActive + "/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io"}`
		}, http.StatusConflict, false},
		{"a domain rule", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsActive + "/registries", `{"display_name": "GitHub", "kind": "oci", "url": "https://ghcr.io", "lookup_from": "data_plane"}`
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

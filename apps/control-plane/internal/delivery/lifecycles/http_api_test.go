package lifecycles

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
		api.WithRoutes(NewLifecycleController(w.svc)), api.WithVerifier(verifier{}))
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
		{http.MethodPost, "/workspaces/" + wsActive + "/lifecycles"},
		{http.MethodGet, "/workspaces/" + wsActive + "/lifecycles"},
		{http.MethodGet, "/lifecycles/lfc_x"},
		{http.MethodPatch, "/lifecycles/lfc_x"},
		{http.MethodPost, "/lifecycles/lfc_x/archive"},
		{http.MethodPost, "/lifecycles/lfc_x/restore"},
		{http.MethodPost, "/lifecycles/lfc_x/phases"},
		{http.MethodPut, "/lifecycles/lfc_x/phases/lfp_x"},
		{http.MethodPut, "/lifecycles/lfc_x/phases/lfp_x/position"},
		{http.MethodDelete, "/lifecycles/lfc_x/phases/lfp_x"},
	} {
		for _, token := range []string{"", "bogus"} {
			rec, _ := h.call(t, op.method, op.path, token, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with token %q", op.method, op.path, token)
		}
	}
	assert.Empty(t, h.authz.asked)
}

func TestLifecyclesOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	staging := h.env(t, wsActive, "staging", false)
	prod := h.env(t, wsActive, "prod", false)

	rec, out := h.call(t, http.MethodPost, "/workspaces/"+wsActive+"/lifecycles", "ada",
		`{"display_name": "Mainline", "description": "The usual path", "phases": [{"name": "Staging", "environment_ids": ["`+staging.ID()+`"]}]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	data := out["data"].(map[string]any)
	id := data["id"].(string)
	assert.Equal(t, "mainline", data["slug"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	require.Len(t, data["phases"], 1)

	rec, out = h.call(t, http.MethodPost, "/lifecycles/"+id+"/phases", "ada", `{"name": "Production", "environment_ids": ["`+prod.ID()+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	phases := out["data"].(map[string]any)["phases"].([]any)
	require.Len(t, phases, 2)
	prodPhase := phases[1].(map[string]any)["id"].(string)

	rec, out = h.call(t, http.MethodPut, "/lifecycles/"+id+"/phases/"+prodPhase, "ada", `{"name": "Prod", "min_environments": 1, "environment_ids": ["`+prod.ID()+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Prod", out["data"].(map[string]any)["phases"].([]any)[1].(map[string]any)["name"])

	rec, out = h.call(t, http.MethodPut, "/lifecycles/"+id+"/phases/"+prodPhase+"/position", "ada", `{"position": 0}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, prodPhase, out["data"].(map[string]any)["phases"].([]any)[0].(map[string]any)["id"])

	rec, out = h.call(t, http.MethodDelete, "/lifecycles/"+id+"/phases/"+prodPhase, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, out["data"].(map[string]any)["phases"], 1)

	rec, out = h.call(t, http.MethodPatch, "/lifecycles/"+id, "ada", `{"display_name": "Main line"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Main line", out["data"].(map[string]any)["display_name"])

	rec, out = h.call(t, http.MethodGet, "/workspaces/"+wsActive+"/lifecycles", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	items := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, id, items[0].(map[string]any)["id"])

	rec, out = h.call(t, http.MethodPost, "/lifecycles/"+id+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, out["data"].(map[string]any)["deleted_at"])

	rec, out = h.call(t, http.MethodPost, "/lifecycles/"+id+"/restore", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, out["data"].(map[string]any)["deleted_at"])

	rec, _ = h.call(t, http.MethodGet, "/lifecycles/"+id, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var types []string
	for _, e := range h.audit {
		types = append(types, e.Type)
	}
	assert.Equal(t, []string{
		delivery.EventLifecycleCreated, delivery.EventLifecycleUpdated, delivery.EventLifecycleUpdated, delivery.EventLifecycleUpdated,
		delivery.EventLifecycleUpdated, delivery.EventLifecycleUpdated, delivery.EventLifecycleArchived, delivery.EventLifecycleRestored,
	}, types)
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
		{"a missing display name is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsActive + "/lifecycles", `{"slug": "mainline"}`
		}, http.StatusUnprocessableEntity, true},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			return http.MethodPost, "/workspaces/" + wsActive + "/lifecycles", `{"display_name": "Mainline"}`
		}, http.StatusForbidden, false},
		{"a missing lifecycle", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/lifecycles/lfc_01JZ0000000000000000000099", ""
		}, http.StatusNotFound, true},
		{"an archived workspace", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + wsArchived + "/lifecycles", `{"display_name": "Mainline"}`
		}, http.StatusConflict, false},
		{"a taken slug", func(h *httpWorld) (string, string, string) {
			h.lifecycle(t, wsActive, "mainline", false)
			return http.MethodPost, "/workspaces/" + wsActive + "/lifecycles", `{"display_name": "Mainline"}`
		}, http.StatusConflict, false},
		{"archiving one in use", func(h *httpWorld) (string, string, string) {
			l := h.lifecycle(t, wsActive, "mainline", false)
			h.channelUse[l.ID()] = 1
			return http.MethodPost, "/lifecycles/" + l.ID() + "/archive", ""
		}, http.StatusConflict, false},
		{"an environment in another workspace", func(h *httpWorld) (string, string, string) {
			e := h.env(t, wsOther, "staging", false)
			return http.MethodPost, "/workspaces/" + wsActive + "/lifecycles", `{"display_name": "Mainline", "phases": [{"name": "s", "environment_ids": ["` + e.ID() + `"]}]}`
		}, http.StatusUnprocessableEntity, false},
		{"an unknown phase", func(h *httpWorld) (string, string, string) {
			l := h.lifecycle(t, wsActive, "mainline", false)
			return http.MethodPut, "/lifecycles/" + l.ID() + "/phases/lfp_01JZ0000000000000000000099/position", `{"position": 0}`
		}, http.StatusNotFound, false},
		{"a domain rule", func(h *httpWorld) (string, string, string) {
			e := h.env(t, wsActive, "staging", false)
			l := h.lifecycle(t, wsActive, "mainline", false, e)
			return http.MethodPut, "/lifecycles/" + l.ID() + "/phases/" + l.Phases()[0].ID + "/position", `{"position": 5}`
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

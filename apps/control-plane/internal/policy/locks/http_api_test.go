package locks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
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
		api.WithRoutes(NewSettingLockController(w.svc)), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	return &httpWorld{world: w, server: s}
}

func (h *httpWorld) call(t *testing.T, method, path, token string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, api.BasePath+path, strings.NewReader(""))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func data(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	d, ok := body["data"].(map[string]any)
	require.True(t, ok, "the body has a data object: %v", body)
	return d
}

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodGet, "/setting-locks"},
		{http.MethodPut, "/setting-locks/" + lifecycleKey},
		{http.MethodDelete, "/setting-locks/" + lifecycleKey},
	} {
		code, _ := h.call(t, op.method, op.path, "")
		assert.Equal(t, http.StatusUnauthorized, code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.asked)

	_, routes := humatest.New(t)
	NewSettingLockController(nil).RegisterRoutes(routes)
	for path, item := range routes.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Delete} {
			if op != nil {
				assert.NotEmpty(t, op.Security, "%s %s needs a token", op.Method, path)
			}
		}
	}
}

func TestSettingLocksOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	atWorkspace := "?workspace_id=" + h.ws.ID()

	code, out := h.call(t, http.MethodPut, "/setting-locks/"+lifecycleKey+atWorkspace, "ada")
	require.Equal(t, http.StatusOK, code, out)
	d := data(t, out)
	assert.Equal(t, lifecycleKey, d["key"])
	assert.Equal(t, h.ws.ID(), d["workspace_id"])
	assert.Equal(t, org, d["organization_id"], "the Organization comes from the token")
	assert.Equal(t, "workspace", d["level"])
	assert.Equal(t, ada.PrincipalID, d["locked_by_principal_id"])

	code, out = h.call(t, http.MethodPut, "/setting-locks/"+lifecycleKey, "ada")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "organization", data(t, out)["level"])

	code, out = h.call(t, http.MethodGet, "/setting-locks"+atWorkspace, "ada")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"], 1, "only the workspace's own locks")

	code, out = h.call(t, http.MethodDelete, "/setting-locks/"+lifecycleKey+atWorkspace, "ada")
	require.Equal(t, http.StatusNoContent, code, out)
	code, out = h.call(t, http.MethodGet, "/setting-locks"+atWorkspace, "ada")
	require.Equal(t, http.StatusOK, code, out)
	assert.Empty(t, out["data"])
	assert.Len(t, h.Locks, 1, "the organization's lock stays")
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		method, path string
		setup        func(h *httpWorld)
		want         int
		wantNotAsked bool
	}{
		{name: "an unknown key", method: http.MethodPut, path: "/setting-locks/delivery.nope", want: http.StatusUnprocessableEntity, wantNotAsked: true},
		{name: "a key not settable below the lock", method: http.MethodPut, path: "/setting-locks/" + workspaceOnlyKey + "?workspace_id=wsp_1", want: http.StatusUnprocessableEntity, wantNotAsked: true},
		{name: "a workspace that does not exist", method: http.MethodPut, path: "/setting-locks/" + lifecycleKey + "?workspace_id=wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", want: http.StatusNotFound},
		// The archived Workspace's ID is only known once the world exists.
		{name: "an archived workspace", method: http.MethodPut, want: http.StatusConflict},
		{name: "removing a lock that is not there", method: http.MethodDelete, path: "/setting-locks/" + lifecycleKey, want: http.StatusNotFound},
		{name: "a refusal", method: http.MethodGet, path: "/setting-locks", setup: func(h *httpWorld) { h.authz.decide = deny }, want: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			path := tc.path
			if path == "" {
				path = "/setting-locks/" + lifecycleKey + "?workspace_id=" + h.archived.ID()
			}
			code, out := h.call(t, tc.method, path, "ada")
			assert.Equal(t, tc.want, code, out)
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.asked)
			}
		})
	}
}

// The controller's operation IDs are its own.
func TestOperationIDs(t *testing.T) {
	_, h := humatest.New(t)
	NewSettingLockController(nil).RegisterRoutes(h)
	var ids []string
	for _, item := range h.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Delete} {
			if op != nil {
				ids = append(ids, op.OperationID)
			}
		}
	}
	assert.ElementsMatch(t, []string{"list-setting-locks", "add-setting-lock", "remove-setting-lock"}, ids)
}

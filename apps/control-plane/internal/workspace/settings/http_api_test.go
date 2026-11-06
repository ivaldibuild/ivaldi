package settings

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspacetest"
)

type httpWorld struct {
	*world
	server *workspacetest.Server
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	w := newWorld(t)
	return &httpWorld{world: w, server: workspacetest.NewServer(t, NewSettingController(w.svc))}
}

func (h *httpWorld) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec, out := h.server.Call(t, method, path, workspacetest.Token, body)
	return rec.Code, out
}

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodGet, "/settings"},
		{http.MethodGet, "/settings/" + lifecycleKey + "/resolved"},
		{http.MethodPut, "/settings/" + lifecycleKey},
		{http.MethodDelete, "/settings/" + lifecycleKey + "?expected_version=1"},
	} {
		rec, _ := h.server.Call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.Asked)
}

func TestSettingsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	atWorkspace := "?workspace_id=" + h.ws.ID()
	atProject := atWorkspace + "&project_id=" + h.project.ID()

	code, out := h.call(t, http.MethodGet, "/settings/"+lifecycleKey+"/resolved"+atProject, "")
	require.Equal(t, http.StatusOK, code, out)
	data := workspacetest.Data(t, out)
	assert.Equal(t, "lfc_basic", data["value"])
	assert.Equal(t, true, data["from_default"])

	code, out = h.call(t, http.MethodPut, "/settings/"+lifecycleKey+atWorkspace, `{"value": "lfc_std", "expected_version": 0}`)
	require.Equal(t, http.StatusOK, code, out)
	data = workspacetest.Data(t, out)
	assert.Equal(t, "lfc_std", data["value"])
	assert.EqualValues(t, 1, data["version"])
	assert.Equal(t, h.ws.ID(), data["scope"].(map[string]any)["workspace_id"])
	assert.Equal(t, org, data["scope"].(map[string]any)["organization_id"], "the Organization comes from the token")

	code, out = h.call(t, http.MethodPut, "/settings/"+lifecycleKey+atWorkspace, `{"value": "lfc_fast", "expected_version": 0}`)
	assert.Equal(t, http.StatusConflict, code, out, "a stale version is refused")

	code, out = h.call(t, http.MethodGet, "/settings"+atWorkspace, "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"], 1)

	code, out = h.call(t, http.MethodGet, "/settings/"+lifecycleKey+"/resolved"+atProject, "")
	require.Equal(t, http.StatusOK, code, out)
	data = workspacetest.Data(t, out)
	assert.Equal(t, "lfc_std", data["value"])
	assert.Equal(t, string(workspace.LevelWorkspace), data["level"])
	assert.Equal(t, false, data["from_default"])

	code, out = h.call(t, http.MethodDelete, "/settings/"+lifecycleKey+atWorkspace+"&expected_version=1", "")
	require.Equal(t, http.StatusNoContent, code, out)
	assert.Empty(t, h.Settings)

	assert.Equal(t, []string{workspace.EventSettingChanged, workspace.EventSettingRemoved}, h.AuditTypes())
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		method, path string
		body         string
		setup        func(h *httpWorld)
		want         int
		wantNotAsked bool
	}{
		{name: "an unknown key", method: http.MethodPut, path: "/settings/delivery.nope", body: `{"value": 1, "expected_version": 0}`, want: http.StatusNotFound, wantNotAsked: true},
		{name: "a value the owning capability rejects", method: http.MethodPut, path: "/settings/" + lifecycleKey, body: `{"value": "env_1", "expected_version": 0}`, want: http.StatusUnprocessableEntity},
		{name: "a level the definition does not allow", method: http.MethodPut, path: "/settings/" + workspaceOnlyKey, body: `{"value": 1, "expected_version": 0}`, want: http.StatusUnprocessableEntity, wantNotAsked: true},
		{name: "a missing value", method: http.MethodPut, path: "/settings/" + lifecycleKey, body: `{"expected_version": 0}`, want: http.StatusUnprocessableEntity, wantNotAsked: true},
		{name: "removing a value that is not there", method: http.MethodDelete, path: "/settings/" + lifecycleKey + "?expected_version=1", want: http.StatusConflict, wantNotAsked: true},
		{name: "a refusal", method: http.MethodGet, path: "/settings", setup: func(h *httpWorld) { h.authz.Decide = workspacetest.Deny }, want: http.StatusForbidden},
		{name: "a project without its workspace", method: http.MethodGet, path: "/settings?project_id=prj_1", want: http.StatusUnprocessableEntity, wantNotAsked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			code, out := h.call(t, tc.method, tc.path, tc.body)
			assert.Equal(t, tc.want, code, out)
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.Asked)
			}
		})
	}
}

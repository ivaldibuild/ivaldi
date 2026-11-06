package workspaces

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
	return &httpWorld{world: w, server: workspacetest.NewServer(t, NewWorkspaceController(w.svc))}
}

func (h *httpWorld) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec, out := h.server.Call(t, method, path, workspacetest.Token, body)
	return rec.Code, out
}

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/workspaces"},
		{http.MethodGet, "/workspaces"},
		{http.MethodGet, "/workspaces/wsp_x"},
		{http.MethodPatch, "/workspaces/wsp_x"},
		{http.MethodPost, "/workspaces/wsp_x/default"},
		{http.MethodPost, "/workspaces/wsp_x/archive"},
		{http.MethodPost, "/workspaces/wsp_x/restore"},
	} {
		rec, _ := h.server.Call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.Asked)
}

func TestWorkspacesOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	def := h.defaultWorkspace(t)

	code, out := h.call(t, http.MethodPost, "/workspaces", `{"slug": "platform", "display_name": "Platform"}`)
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "success", out["status"])
	data := workspacetest.Data(t, out)
	assert.Equal(t, "platform", data["slug"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	assert.Equal(t, alice, h.authz.Last().Actor)
	id := data["id"].(string)

	code, out = h.call(t, http.MethodPatch, "/workspaces/"+id, `{"description": "Core services"}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "Core services", workspacetest.Data(t, out)["description"])

	code, out = h.call(t, http.MethodGet, "/workspaces/"+id, "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, id, workspacetest.Data(t, out)["id"])

	code, out = h.call(t, http.MethodPost, "/workspaces/"+id+"/default", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, true, workspacetest.Data(t, out)["is_default"])

	code, out = h.call(t, http.MethodPost, "/workspaces/"+def.ID()+"/archive", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.NotNil(t, workspacetest.Data(t, out)["archived_at"])

	code, out = h.call(t, http.MethodGet, "/workspaces", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"], 1, "archived workspaces are left out")
	code, out = h.call(t, http.MethodGet, "/workspaces?include_archived=true", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"], 2)

	code, out = h.call(t, http.MethodPost, "/workspaces/"+def.ID()+"/restore", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Nil(t, workspacetest.Data(t, out)["archived_at"])

	assert.Equal(t, []string{
		workspace.EventWorkspaceCreated, workspace.EventWorkspaceCreated, workspace.EventWorkspaceUpdated,
		workspace.EventWorkspaceDefaultMoved, workspace.EventWorkspaceArchived, workspace.EventWorkspaceRestored,
	}, h.AuditTypes())
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, h *httpWorld) (method, path, body string)
		want         int
		wantNotAsked bool
	}{
		{"an invalid slug is refused before asking", func(*testing.T, *httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces", `{"slug": "Bad Slug", "display_name": "P"}`
		}, http.StatusUnprocessableEntity, true},
		{"a refusal", func(_ *testing.T, h *httpWorld) (string, string, string) {
			h.authz.Decide = workspacetest.Deny
			return http.MethodPost, "/workspaces", `{"slug": "platform", "display_name": "Platform"}`
		}, http.StatusForbidden, false},
		{"a missing workspace", func(*testing.T, *httpWorld) (string, string, string) {
			return http.MethodGet, "/workspaces/wsp_01JZ0000000000000000000099", ""
		}, http.StatusNotFound, false},
		{"a taken slug", func(t *testing.T, h *httpWorld) (string, string, string) {
			h.workspace(t, "platform")
			return http.MethodPost, "/workspaces", `{"slug": "platform", "display_name": "Platform"}`
		}, http.StatusConflict, false},
		{"archiving the default", func(t *testing.T, h *httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + h.defaultWorkspace(t).ID() + "/archive", ""
		}, http.StatusConflict, false},
		{"archiving a workspace with projects", func(t *testing.T, h *httpWorld) (string, string, string) {
			ws := h.workspace(t, "platform")
			h.project(t, ws, "checkout")
			return http.MethodPost, "/workspaces/" + ws.ID() + "/archive", ""
		}, http.StatusConflict, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(t, h)
			h.authz.Asked = nil
			code, out := h.call(t, method, path, body)
			assert.Equal(t, tc.want, code, out)
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.Asked)
			}
		})
	}
}

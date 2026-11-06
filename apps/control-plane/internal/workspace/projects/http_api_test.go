package projects

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
	return &httpWorld{world: w, server: workspacetest.NewServer(t, NewProjectController(w.svc))}
}

func (h *httpWorld) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec, out := h.server.Call(t, method, path, workspacetest.Token, body)
	return rec.Code, out
}

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/workspaces/wsp_x/projects"},
		{http.MethodGet, "/workspaces/wsp_x/projects"},
		{http.MethodGet, "/projects/prj_x"},
		{http.MethodPatch, "/projects/prj_x"},
		{http.MethodPost, "/projects/prj_x/transfer"},
		{http.MethodPost, "/projects/prj_x/archive"},
		{http.MethodPost, "/projects/prj_x/restore"},
	} {
		rec, _ := h.server.Call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.Asked)
}

func TestProjectsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	ws := h.workspace(t, "platform", false)

	code, out := h.call(t, http.MethodPost, "/workspaces/"+ws.ID()+"/projects", `{"owner_team_id": "tem_1", "slug": "checkout", "display_name": "Checkout"}`)
	require.Equal(t, http.StatusCreated, code, out)
	data := workspacetest.Data(t, out)
	assert.Equal(t, ws.ID(), data["workspace_id"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	id := data["id"].(string)

	code, out = h.call(t, http.MethodPatch, "/projects/"+id, `{"display_name": "Payments"}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "Payments", workspacetest.Data(t, out)["display_name"])

	code, out = h.call(t, http.MethodPost, "/projects/"+id+"/transfer", `{"owner_team_id": "tem_2"}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "tem_2", workspacetest.Data(t, out)["owner_team_id"])

	code, out = h.call(t, http.MethodGet, "/workspaces/"+ws.ID()+"/projects", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"], 1)

	code, out = h.call(t, http.MethodPost, "/projects/"+id+"/archive", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.NotNil(t, workspacetest.Data(t, out)["archived_at"])

	code, out = h.call(t, http.MethodPost, "/projects/"+id+"/restore", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Nil(t, workspacetest.Data(t, out)["archived_at"])

	code, out = h.call(t, http.MethodGet, "/projects/"+id, "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, id, workspacetest.Data(t, out)["id"])

	assert.Equal(t, []string{
		workspace.EventProjectCreated, workspace.EventProjectUpdated, workspace.EventProjectOwnerChanged,
		workspace.EventProjectArchived, workspace.EventProjectRestored,
	}, h.AuditTypes())
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, h *httpWorld) (method, path, body string)
		want  int
	}{
		{"an inactive team", func(t *testing.T, h *httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + h.workspace(t, "platform", false).ID() + "/projects", `{"owner_team_id": "tem_gone", "slug": "checkout", "display_name": "Checkout"}`
		}, http.StatusUnprocessableEntity},
		{"an archived workspace", func(t *testing.T, h *httpWorld) (string, string, string) {
			return http.MethodPost, "/workspaces/" + h.workspace(t, "platform", true).ID() + "/projects", `{"owner_team_id": "tem_1", "slug": "checkout", "display_name": "Checkout"}`
		}, http.StatusConflict},
		{"a refusal", func(t *testing.T, h *httpWorld) (string, string, string) {
			h.authz.Decide = workspacetest.Deny
			return http.MethodPost, "/workspaces/" + h.workspace(t, "platform", false).ID() + "/projects", `{"owner_team_id": "tem_1", "slug": "checkout", "display_name": "Checkout"}`
		}, http.StatusForbidden},
		{"a missing project", func(*testing.T, *httpWorld) (string, string, string) {
			return http.MethodPost, "/projects/prj_01JZ0000000000000000000099/restore", ""
		}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(t, h)
			code, out := h.call(t, method, path, body)
			assert.Equal(t, tc.want, code, out)
		})
	}
}

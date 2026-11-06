package workspace_test

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/projects"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/settings"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspaces"
)

// The sub-packages' controllers are served side by side, so their routes
// and operation IDs must not clash, and every operation needs a token.
func TestControllersFitTogether(t *testing.T) {
	_, h := humatest.New(t)
	for _, r := range []api.Routes{
		workspaces.NewWorkspaceController(nil),
		projects.NewProjectController(nil),
		settings.NewSettingController(nil),
	} {
		r.RegisterRoutes(h)
	}

	ids := map[string]string{}
	for path, item := range h.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{
			"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete,
		} {
			if op == nil {
				continue
			}
			where := method + " " + path
			if other, ok := ids[op.OperationID]; ok {
				t.Errorf("operation ID %q is used by %s and %s", op.OperationID, other, where)
			}
			ids[op.OperationID] = where
			assert.NotEmpty(t, op.Security, "%s needs a token", where)
		}
	}
	assert.Len(t, ids, 7+7+4)
}

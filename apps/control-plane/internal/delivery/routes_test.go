package delivery_test

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/agentapi"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/applications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/clusters"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/deployments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/environments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/lifecycles"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/registries"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/releases"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/variables"
)

// The sub-packages' controllers are served side by side, so their routes
// and operation IDs must not clash, and every operation needs a token.
func TestControllersFitTogether(t *testing.T) {
	_, h := humatest.New(t)
	for _, r := range []api.Routes{
		environments.NewEnvironmentController(nil),
		clusters.NewController(nil),
		lifecycles.NewLifecycleController(nil),
		registries.NewRegistryController(nil),
		applications.NewApplicationController(nil),
		releases.NewReleaseController(nil),
		deployments.NewController(nil),
		agentapi.NewController(nil),
		variables.NewVariableController(nil),
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
	assert.Len(t, ids, 13+6+10+8+32+6+5+4+10)
}

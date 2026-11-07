package deployments

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
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

type httpVerifier struct{}

func (httpVerifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	switch token {
	case "ada":
		return &identity.Claims{Subject: ada.PrincipalID, Kind: ada.Kind, OrganizationID: org}, nil
	case "agent":
		return &identity.Claims{Subject: agentID, Kind: identity.ActorServiceAccount, OrganizationID: org}, nil
	default:
		return nil, identity.ErrInvalidToken
	}
}

func TestOverHTTP(t *testing.T) {
	w := newWorld(t)
	port := 0
	server, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(httpVerifier{}))
	require.NoError(t, err)
	call := func(method, path, token, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, api.BasePath+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, _ := call(http.MethodPost, "/releases/"+w.rel.ID()+"/deployments", "", `{"target_ids":["`+targetID+`"]}`)
	assert.Equal(t, http.StatusUnauthorized, code)

	code, out := call(http.MethodPost, "/releases/"+w.rel.ID()+"/deployments", "ada", `{"target_ids":["`+targetID+`"]}`)
	require.Equal(t, http.StatusCreated, code, out)
	items := out["data"].([]any)
	require.Len(t, items, 1)
	deployment := items[0].(map[string]any)
	id := deployment["id"].(string)
	assert.Equal(t, "applying", deployment["status"])

	code, out = call(http.MethodGet, "/deployments/"+id, "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, id, out["data"].(map[string]any)["deployment"].(map[string]any)["id"])

	code, out = call(http.MethodGet, "/applications/"+appID+"/deployments", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["data"].(map[string]any)["items"].([]any), 1)

	code, out = call(http.MethodGet, "/releases/"+w.rel.ID()+"/changes?target_id="+targetID, "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEmpty(t, out["data"].(map[string]any)["checks"].([]any))

	code, _ = call(http.MethodPost, "/deployments/"+id+"/cancel", "ada", "")
	assert.Equal(t, http.StatusConflict, code, "an applying deployment cannot be cancelled")

	awaiting := mustDeployment(org, w.rel.ID(), w.app.ID(), envID, targetID, release.DeploymentAwaitingApproval)
	w.deployments[awaiting.ID()] = awaiting
	code, out = call(http.MethodPost, "/deployments/"+awaiting.ID()+"/cancel", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "cancelled", out["data"].(map[string]any)["deployment"].(map[string]any)["status"])

	code, _ = call(http.MethodPost, "/targets/"+targetID+"/reports", "agent", "")
	assert.Equal(t, http.StatusNoContent, code)
}

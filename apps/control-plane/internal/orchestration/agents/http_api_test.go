package agents

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
)

// verifier accepts the token "ada".
type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada.PrincipalID, Kind: ada.Kind, OrganizationID: org}, nil
}

func TestOverHTTP(t *testing.T) {
	w := newWorld(t)
	port := 0
	server, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(verifier{}))
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

	for _, op := range [][2]string{
		{http.MethodPost, "/agents"}, {http.MethodGet, "/agents"}, {http.MethodGet, "/agents/x"},
		{http.MethodPatch, "/agents/x"}, {http.MethodPost, "/agents/x/suspend"}, {http.MethodPost, "/agents/x/reactivate"},
	} {
		code, _ := call(op[0], op[1], "", "")
		assert.Equal(t, http.StatusUnauthorized, code, "%s %s", op[0], op[1])
	}

	code, out := call(http.MethodPost, "/agents", "ada",
		`{"display_name":"Release helper","team_id":"`+teamID+`","system_prompt":"Ship carefully."}`)
	require.Equal(t, http.StatusCreated, code, out)
	created := out["data"].(map[string]any)
	id := created["id"].(string)
	assert.Equal(t, "Ship carefully.", created["system_prompt"])

	code, out = call(http.MethodGet, "/agents", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	listed := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, listed, 1)
	assert.Equal(t, true, listed[0].(map[string]any)["has_system_prompt"])

	code, out = call(http.MethodGet, "/agents/"+id, "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "Ship carefully.", out["data"].(map[string]any)["system_prompt"])

	code, out = call(http.MethodPatch, "/agents/"+id, "ada", `{"system_prompt":"Prefer the graph."}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "Prefer the graph.", out["data"].(map[string]any)["system_prompt"])

	code, out = call(http.MethodPost, "/agents/"+id+"/suspend", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "suspended", out["data"].(map[string]any)["status"])

	code, out = call(http.MethodPost, "/agents/"+id+"/reactivate", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "active", out["data"].(map[string]any)["status"])

	code, _ = call(http.MethodPost, "/agents", "ada", `{"display_name":"No Team"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code, "team_id is required")
}

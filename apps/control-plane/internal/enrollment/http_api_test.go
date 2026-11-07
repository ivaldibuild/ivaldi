package enrollment

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

type testVerifier struct{ w *world }

func (v testVerifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	switch token {
	case "ada":
		return &identity.Claims{Subject: ada.PrincipalID, Kind: identity.ActorUser, OrganizationID: org}, nil
	case "workload":
		for _, c := range v.w.credentials {
			return &identity.Claims{
				Subject: c.PrincipalID(), OrganizationID: c.OrganizationID(), Kind: identity.ActorWorkload,
				ID: c.ID(), ExpiresAt: *c.NotAfter(),
			}, nil
		}
	}
	return nil, identity.ErrInvalidToken
}

func TestOverHTTP(t *testing.T) {
	w := newWorld(t)
	port := 0
	server, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(testVerifier{w}))
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

	code, _ := call(http.MethodPost, "/targets/"+targetID+"/enrolments", "", `{"method":"bearer"}`)
	assert.Equal(t, http.StatusUnauthorized, code)

	code, out := call(http.MethodPost, "/targets/"+targetID+"/enrolments", "ada", `{"method":"bearer"}`)
	require.Equal(t, http.StatusCreated, code, out)
	token := out["data"].(map[string]any)["token"].(string)

	code, out = call(http.MethodPost, "/agent/enrolments/redeem", "", `{"token":"`+token+`","method":"bearer"}`)
	require.Equal(t, http.StatusCreated, code, out)
	data := out["data"].(map[string]any)
	assert.Contains(t, data["workload_token"], "iwt_")

	code, out = call(http.MethodGet, "/targets/"+targetID+"/agent", "ada", "")
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "active", out["data"].(map[string]any)["status"])

	code, out = call(http.MethodPost, "/agent/credentials/renew", "workload", `{}`)
	require.Equal(t, http.StatusOK, code, out)
	assert.Contains(t, out["data"].(map[string]any)["workload_token"], "iwt_")
}

package clusters

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
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(verifier{}))
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

func TestEveryClusterOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodPost, "/clusters"},
		{http.MethodGet, "/clusters"},
		{http.MethodGet, "/clusters/cls_x"},
		{http.MethodPatch, "/clusters/cls_x"},
		{http.MethodPost, "/clusters/cls_x/archive"},
		{http.MethodPost, "/clusters/cls_x/restore"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
	assert.Empty(t, h.authz.asked)
}

func TestCreateClusterOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodPost, "/clusters", "ada",
		`{"display_name": "Prod EU", "runtime": "kubernetes"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	data := out["data"].(map[string]any)
	assert.Equal(t, "prod-eu", data["slug"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	require.Len(t, h.authz.asked, 1)
	assert.Equal(t, ada, h.authz.asked[0].Actor)
}

func TestClusterHTTPErrors(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *httpWorld) (method, path, body string)
		want  int
	}{
		{"an unknown runtime is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/clusters", `{"display_name": "Prod EU", "runtime": "nomad"}`
		}, http.StatusUnprocessableEntity},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			return http.MethodPost, "/clusters", `{"display_name": "Prod EU", "runtime": "kubernetes"}`
		}, http.StatusForbidden},
		{"a missing cluster", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/clusters/cls_01JZ0000000000000000000099", ""
		}, http.StatusNotFound},
		{"a duplicate slug", func(h *httpWorld) (string, string, string) {
			h.cluster(t, "prod-eu", "", false)
			return http.MethodPost, "/clusters", `{"display_name": "Prod EU", "runtime": "kubernetes"}`
		}, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(h)
			rec, _ := h.call(t, method, path, "ada", body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

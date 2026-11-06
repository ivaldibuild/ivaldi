package grants

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

// verifier accepts the token "ada", for Ada in Acme.
type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: ada, Kind: identity.ActorUser, OrganizationID: org}, nil
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
		api.WithRoutes(NewGrantController(w.svc)), api.WithVerifier(verifier{}))
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

func TestEveryOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodGet, "/grants?resource_id=" + org},
		{http.MethodPost, "/grants"},
		{http.MethodDelete, "/grants/grt_x"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
		rec, _ = h.call(t, op.method, op.path, "forged", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with a bad token", op.method, op.path)
	}
}

func TestGrantsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodPost, "/grants", "ada", `{"team_id":"`+platform+`","role":"member","resource_id":"`+workspace+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	g := out["data"].(map[string]any)
	assert.Equal(t, "member", g["role"])
	assert.Nil(t, g["principal"])
	group := g["group"].(map[string]any)
	assert.Equal(t, teamGroup, group["id"])
	assert.ElementsMatch(t, []string{"id", "role", "role_id", "resource_id", "principal", "group", "granted_by_principal_id", "expires_at", "created_at"}, keys(g))
	grantID := g["id"].(string)

	rec, out = h.call(t, http.MethodGet, "/grants?resource_id="+workspace+"&include_ancestors=true", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	items := out["data"].(map[string]any)["items"].([]any)
	assert.Len(t, items, 2, "the Team's grant and the owner's, from the Organization")

	rec, _ = h.call(t, http.MethodDelete, "/grants/"+grantID, "ada", "")
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec, _ = h.call(t, http.MethodDelete, "/grants/"+grantID, "ada", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGrantErrorsOverHTTP(t *testing.T) {
	cases := map[string]struct {
		method, path, body string
		deny               bool
		want               int
	}{
		"an unknown role":       {method: http.MethodPost, path: "/grants", body: `{"principal_id":"` + grace + `","role":"root","resource_id":"` + org + `"}`, want: http.StatusUnprocessableEntity},
		"owner on a Workspace":  {method: http.MethodPost, path: "/grants", body: `{"principal_id":"` + grace + `","role":"owner","resource_id":"` + workspace + `"}`, want: http.StatusUnprocessableEntity},
		"a grant that exists":   {method: http.MethodPost, path: "/grants", body: `{"principal_id":"` + ada + `","role":"owner","resource_id":"` + org + `"}`, want: http.StatusConflict},
		"a suspended principal": {method: http.MethodPost, path: "/grants", body: `{"principal_id":"` + linus + `","role":"member","resource_id":"` + org + `"}`, want: http.StatusConflict},
		"denied":                {method: http.MethodPost, path: "/grants", body: `{"principal_id":"` + grace + `","role":"member","resource_id":"` + org + `"}`, deny: true, want: http.StatusForbidden},
		"an unknown resource":   {method: http.MethodGet, path: "/grants?resource_id=wsp_01JZ0000000000000000000099", want: http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHTTPWorld(t)
			if tc.deny {
				h.authz.deny = func(authorization.Request) bool { return true }
			}
			rec, _ := h.call(t, tc.method, tc.path, "ada", tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}

	t.Run("the last owner", func(t *testing.T) {
		h := newHTTPWorld(t)
		rec, out := h.call(t, http.MethodDelete, "/grants/"+h.owner.ID(), "ada", "")
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, out["detail"], "last owner")
		assert.Contains(t, h.grants, h.owner.ID())
	})
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

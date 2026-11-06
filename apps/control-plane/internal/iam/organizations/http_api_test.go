package organizations

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
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// verifier accepts the token "ada", for Ada in Acme.
type verifier struct{ w *world }

func (v verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ada" {
		return nil, identity.ErrInvalidToken
	}
	return &identity.Claims{Subject: v.w.ada.PrincipalID, Kind: v.w.ada.Kind, OrganizationID: v.w.acme}, nil
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
		api.WithRoutes(NewOrganizationController(w.svc)), api.WithVerifier(verifier{w}))
	require.NoError(t, err)
	return &httpWorld{world: w, server: s}
}

func (h *httpWorld) call(t *testing.T, method, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, api.BasePath+"/organization", strings.NewReader(body))
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
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		rec, _ := h.call(t, method, "", `{}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, method)
		rec, _ = h.call(t, method, "forged", `{}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, method+" with a bad token")
	}
}

func TestGetOrganizationOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	rec, out := h.call(t, http.MethodGet, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data := out["data"].(map[string]any)
	assert.Equal(t, h.acme, data["id"], "the token's Organization")
	assert.Equal(t, "acme", data["slug"])
	assert.Equal(t, "Acme Corporation", data["name"])
	assert.Equal(t, "Acme", data["display_name"])
	assert.Nil(t, data["logo_url"])
	assert.ElementsMatch(t, []string{"id", "slug", "name", "display_name", "logo_url", "created_at", "updated_at"}, keys(data))
}

func TestUpdateOrganizationOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	rec, out := h.call(t, http.MethodPatch, "ada", `{"display_name":"Acme Inc.","slug":"acme-inc","logo_url":"https://acme.example.com/logo.png"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	data := out["data"].(map[string]any)
	assert.Equal(t, "Acme Inc.", data["display_name"])
	assert.Equal(t, "acme-inc", data["slug"])
	assert.Equal(t, "Acme Corporation", data["name"], "an omitted field is unchanged")
	assert.Equal(t, "https://acme.example.com/logo.png", data["logo_url"])
	assert.Equal(t, "acme-inc", h.orgs[h.acme].Slug)
}

func TestUpdateOrganizationErrorsOverHTTP(t *testing.T) {
	cases := map[string]struct {
		body  string
		setup func(*world)
		want  int
	}{
		"a slug in use":            {body: `{"slug":"globex"}`, want: http.StatusConflict},
		"an invalid slug":          {body: `{"slug":"Not A Slug"}`, want: http.StatusUnprocessableEntity},
		"an empty display name":    {body: `{"display_name":""}`, want: http.StatusUnprocessableEntity},
		"a logo that is not https": {body: `{"logo_url":"http://acme.example.com/logo.png"}`, want: http.StatusUnprocessableEntity},
		"not an owner or admin": {
			body: `{"display_name":"Acme Inc."}`,
			setup: func(w *world) {
				w.authz.deny = func(r authorization.Request) bool { return r.Permission == iam.PermissionOrganizationManage }
			},
			want: http.StatusForbidden,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHTTPWorld(t)
			if tc.setup != nil {
				tc.setup(h.world)
			}
			rec, _ := h.call(t, http.MethodPatch, "ada", tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Equal(t, "acme", h.orgs[h.acme].Slug)
		})
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

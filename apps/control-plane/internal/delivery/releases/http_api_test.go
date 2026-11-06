package releases

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

// verifier accepts the token "ci", for a CI pipeline acting as Ada.
type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	if token != "ci" {
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
		api.WithRoutes(NewReleaseController(w.svc)), api.WithVerifier(verifier{}))
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
		{http.MethodPost, "/applications/app_x/builds"},
		{http.MethodPost, "/applications/app_x/releases"},
		{http.MethodGet, "/applications/app_x/releases"},
		{http.MethodGet, "/releases/rel_x"},
		{http.MethodPost, "/releases/rel_x/publish"},
		{http.MethodPost, "/releases/rel_x/withdraw"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
}

// tagPipeline is the body CI sends from a tag: one call, published.
func tagPipeline(version, c string) string {
	return `{"version":"` + version + `","publish":true,"builds":[{"artifact":"api","digest":"` + digest(c) + `",
		"version":"` + version + `","commit_sha":"` + strings.Repeat(c, 40) + `","source_ref":"refs/tags/v` + version + `",
		"ci_provider":"github_actions","ci_run_ref":"https://github.com/acme/api/actions/runs/7","tags":["v` + version + `"]}]}`
}

func TestCreateReleaseFromCI(t *testing.T) {
	h := newHTTPWorld(t)
	appPath := "/applications/" + h.app.ID()

	rec, out := h.call(t, http.MethodPost, appPath+"/releases", "ci", tagPipeline("1.4.0", "a"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rel := out["data"].(map[string]any)
	assert.Equal(t, "published", rel["status"])
	assert.Equal(t, h.main.ID(), rel["channel_id"])
	assert.NotEmpty(t, rel["publish_decision_id"])

	builds := rel["builds"].([]any)
	require.Len(t, builds, 1)
	item := builds[0].(map[string]any)
	assert.Equal(t, h.api.ID(), item["artifact_id"])
	build := item["build"].(map[string]any)
	assert.Equal(t, digest("a"), build["digest"])
	assert.Equal(t, "refs/tags/v1.4.0", build["source_ref"])
	assert.Equal(t, []any{"v1.4.0"}, build["reported_tags"])

	t.Run("reads it back", func(t *testing.T) {
		rec, out := h.call(t, http.MethodGet, "/releases/"+rel["id"].(string), "ci", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "1.4.0", out["data"].(map[string]any)["version"])

		rec, out = h.call(t, http.MethodGet, appPath+"/releases?status=published&channel_id="+h.main.ID(), "ci", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Len(t, out["data"].(map[string]any)["items"], 1)
	})

	t.Run("withdraws it", func(t *testing.T) {
		rec, out := h.call(t, http.MethodPost, "/releases/"+rel["id"].(string)+"/withdraw", "ci", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "withdrawn", out["data"].(map[string]any)["status"])
	})

	t.Run("a draft from a pull request, published later", func(t *testing.T) {
		body := `{"version":"1.5.0-beta.42","builds":[{"artifact":"api","digest":"` + digest("b") + `","source_ref":"refs/pull/42/head","commit_sha":"` + strings.Repeat("b", 40) + `"}]}`
		rec, out := h.call(t, http.MethodPost, appPath+"/releases", "ci", body)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		draft := out["data"].(map[string]any)
		assert.Equal(t, "draft", draft["status"])
		assert.Equal(t, h.beta.ID(), draft["channel_id"])

		rec, out = h.call(t, http.MethodPost, "/releases/"+draft["id"].(string)+"/publish", "ci", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "published", out["data"].(map[string]any)["status"])
	})
}

func TestReportBuildOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	rec, out := h.call(t, http.MethodPost, "/applications/"+h.app.ID()+"/builds", "ci",
		`{"artifact":"api","digest":"`+digest("a")+`","commit_sha":"`+strings.Repeat("a", 40)+`","source_ref":"refs/heads/main"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "ci_reported", out["data"].(map[string]any)["provenance"])

	rec, out = h.call(t, http.MethodPost, "/applications/"+h.app.ID()+"/builds", "ci",
		`{"artifact":"api","digest":"`+digest("a")+`","commit_sha":"`+strings.Repeat("f", 40)+`"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, out["detail"], "was built from commit")
}

func TestReleaseErrorsOverHTTP(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
		detail string
	}{
		"rules no channel meets": {
			body:   `{"version":"2.0.0-rc.1","builds":[{"artifact":"api","digest":"` + digest("a") + `"}]}`,
			status: http.StatusUnprocessableEntity, detail: "no channel's rules admit the release",
		},
		"a tag instead of a digest": {
			body:   `{"version":"1.0.0","builds":[{"artifact":"api","tag":"v1.0.0"}]}`,
			status: http.StatusUnprocessableEntity, detail: "give the digest",
		},
		"no builds": {
			body:   `{"version":"1.0.0","builds":[]}`,
			status: http.StatusUnprocessableEntity,
		},
		"an unknown artifact": {
			body:   `{"version":"1.0.0","builds":[{"artifact":"web","digest":"` + digest("a") + `"}]}`,
			status: http.StatusUnprocessableEntity, detail: `no active artifact named "web"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHTTPWorld(t)
			rec, out := h.call(t, http.MethodPost, "/applications/"+h.app.ID()+"/releases", "ci", tc.body)
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.detail != "" {
				assert.Contains(t, out["detail"], tc.detail)
			}
		})
	}

	t.Run("a version that exists", func(t *testing.T) {
		h := newHTTPWorld(t)
		rec, _ := h.call(t, http.MethodPost, "/applications/"+h.app.ID()+"/releases", "ci", tagPipeline("1.4.0", "a"))
		require.Equal(t, http.StatusCreated, rec.Code)
		rec, out := h.call(t, http.MethodPost, "/applications/"+h.app.ID()+"/releases", "ci", tagPipeline("1.4.0", "b"))
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, out["detail"], "already has a release 1.4.0")
	})
}

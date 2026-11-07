package applications

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
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
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
		api.WithRoutes(NewApplicationController(w.svc)), api.WithVerifier(verifier{}))
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
		{http.MethodPost, "/projects/" + prjActive + "/applications"},
		{http.MethodGet, "/projects/" + prjActive + "/applications"},
		{http.MethodGet, "/workspaces/" + wsActive + "/applications"},
		{http.MethodGet, "/applications/app_x"},
		{http.MethodPatch, "/applications/app_x"},
		{http.MethodPut, "/applications/app_x/source"},
		{http.MethodPost, "/applications/app_x/transfer"},
		{http.MethodPost, "/applications/app_x/archive"},
		{http.MethodPost, "/applications/app_x/restore"},
		{http.MethodPost, "/applications/app_x/artifacts"},
		{http.MethodGet, "/applications/app_x/artifacts"},
		{http.MethodPatch, "/artifacts/art_x"},
		{http.MethodPut, "/artifacts/art_x/registry"},
		{http.MethodPost, "/artifacts/art_x/declare"},
		{http.MethodPost, "/artifacts/art_x/archive"},
		{http.MethodPost, "/artifacts/art_x/restore"},
		{http.MethodPost, "/applications/app_x/channels"},
		{http.MethodGet, "/applications/app_x/channels"},
		{http.MethodGet, "/channels/chn_x"},
		{http.MethodPatch, "/channels/chn_x"},
		{http.MethodPut, "/channels/chn_x/lifecycle"},
		{http.MethodPost, "/channels/chn_x/default"},
		{http.MethodPost, "/channels/chn_x/archive"},
		{http.MethodPost, "/channels/chn_x/restore"},
		{http.MethodPost, "/channels/chn_x/rules"},
		{http.MethodPut, "/channels/chn_x/rules/chr_x"},
		{http.MethodDelete, "/channels/chn_x/rules/chr_x"},
		{http.MethodPost, "/applications/app_x/placements"},
		{http.MethodGet, "/applications/app_x/placements"},
		{http.MethodPut, "/applications/app_x/placements/tgt_x"},
		{http.MethodDelete, "/applications/app_x/placements/tgt_x"},
	} {
		for _, token := range []string{"", "bogus"} {
			rec, _ := h.call(t, op.method, op.path, token, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with token %q", op.method, op.path, token)
		}
	}
	assert.Empty(t, h.authz.asked)
}

func data(out map[string]any) map[string]any {
	return out["data"].(map[string]any)
}

func TestApplicationsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)
	l := h.lifecycle(t, wsActive, "mainline", false)
	next := h.lifecycle(t, wsActive, "next", false)
	reg := h.registry(t, wsActive, "ghcr", false)
	tg := h.target(t, h.env(t, wsActive, "staging"), "web")

	rec, out := h.call(t, http.MethodPost, "/projects/"+prjActive+"/applications", "ada",
		`{"display_name": "Checkout API", "default_lifecycle_id": "`+l.ID()+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	app := data(out)["application"].(map[string]any)
	appID := app["id"].(string)
	assert.Equal(t, "checkout-api", app["slug"])
	assert.Equal(t, org, app["organization_id"], "the Organization comes from the token")
	assert.Equal(t, wsActive, app["workspace_id"])
	defaultCh := data(out)["default_channel"].(map[string]any)
	assert.Equal(t, true, defaultCh["is_default"])

	rec, out = h.call(t, http.MethodPatch, "/applications/"+appID, "ada", `{"display_name": "Checkout"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Checkout", data(out)["display_name"])

	rec, out = h.call(t, http.MethodPut, "/applications/"+appID+"/source", "ada", `{"source_id": "src_1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "src_1", data(out)["source_id"])

	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/transfer", "ada", `{"owner_team_id": "`+teamOther+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, teamOther, data(out)["owner_team_id"])

	for _, path := range []string{"/projects/" + prjActive + "/applications", "/workspaces/" + wsActive + "/applications?owner_team_id=" + teamOther} {
		rec, out = h.call(t, http.MethodGet, path, "ada", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		items := data(out)["items"].([]any)
		require.Len(t, items, 1, path)
		assert.Equal(t, appID, items[0].(map[string]any)["id"])
	}

	// Artifacts.
	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/artifacts", "ada",
		`{"name": "chart", "artifact_kind": "helm_chart", "artifact_name": "ghcr.io/acme/chart"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	artID := data(out)["id"].(string)

	rec, out = h.call(t, http.MethodPatch, "/artifacts/"+artID, "ada", `{"required": true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, true, data(out)["required"])

	rec, out = h.call(t, http.MethodPut, "/artifacts/"+artID+"/registry", "ada", `{"registry_id": "`+reg.ID()+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, reg.ID(), data(out)["registry_id"])

	rec, out = h.call(t, http.MethodGet, "/applications/"+appID+"/artifacts", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, out["data"], 1)

	// Channels.
	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/channels", "ada",
		`{"display_name": "Hotfix", "lifecycle_id": "`+l.ID()+`", "rules": [{"kind": "git_ref", "artifact_id": "`+artID+`", "config": {"patterns": ["refs/heads/main"]}}]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	chID := data(out)["id"].(string)
	rules := data(out)["rules"].([]any)
	require.Len(t, rules, 1)
	ruleID := rules[0].(map[string]any)["id"].(string)
	assert.Equal(t, map[string]any{"patterns": []any{"refs/heads/main"}}, rules[0].(map[string]any)["config"], "the rule's config is JSON, not a string")

	rec, out = h.call(t, http.MethodPost, "/channels/"+chID+"/rules", "ada", `{"kind": "version", "config": {"pattern": "^v"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, data(out)["rules"], 2)

	rec, _ = h.call(t, http.MethodPut, "/channels/"+chID+"/rules/"+ruleID, "ada", `{"kind": "git_ref", "config": {"patterns": ["refs/heads/release"]}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec, out = h.call(t, http.MethodDelete, "/channels/"+chID+"/rules/"+ruleID, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, data(out)["rules"], 1)

	rec, out = h.call(t, http.MethodPut, "/channels/"+chID+"/lifecycle", "ada", `{"lifecycle_id": "`+next.ID()+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, next.ID(), data(out)["lifecycle_id"])

	rec, out = h.call(t, http.MethodPost, "/channels/"+chID+"/default", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, true, data(out)["is_default"])

	rec, out = h.call(t, http.MethodPost, "/channels/"+defaultCh["id"].(string)+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, data(out)["archived_at"])

	rec, out = h.call(t, http.MethodGet, "/applications/"+appID+"/channels?include_archived=true", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, out["data"], 2)

	// Placements.
	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/placements", "ada",
		`{"target_id": "`+tg.ID()+`", "apply_config": {"definition_artifact": "chart"}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "chart", data(out)["apply_config"].(map[string]any)["definition_artifact"])

	rec, out = h.call(t, http.MethodPut, "/applications/"+appID+"/placements/"+tg.ID(), "ada", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, data(out)["apply_config"], "no config makes deployments there tracked only")

	rec, out = h.call(t, http.MethodGet, "/applications/"+appID+"/placements", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, out["data"], 1)

	rec, _ = h.call(t, http.MethodDelete, "/applications/"+appID+"/placements/"+tg.ID(), "ada", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, h.placements)

	// The Application itself.
	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, data(out)["archived_at"])

	rec, out = h.call(t, http.MethodPost, "/applications/"+appID+"/restore", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, data(out)["archived_at"])

	rec, _ = h.call(t, http.MethodGet, "/applications/"+appID, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{
		delivery.EventApplicationCreated, delivery.EventChannelCreated, delivery.EventApplicationUpdated, delivery.EventApplicationUpdated,
		delivery.EventApplicationOwnerChanged, delivery.EventArtifactCreated, delivery.EventArtifactUpdated, delivery.EventArtifactUpdated,
		delivery.EventChannelCreated, delivery.EventChannelUpdated, delivery.EventChannelUpdated, delivery.EventChannelUpdated,
		delivery.EventChannelUpdated, delivery.EventChannelDefaultMoved, delivery.EventChannelArchived,
		delivery.EventPlacementCreated, delivery.EventPlacementUpdated, delivery.EventPlacementRemoved,
		delivery.EventApplicationArchived, delivery.EventApplicationRestored,
	}, h.auditTypes())
	for _, req := range h.authz.asked {
		assert.Equal(t, ada, req.Actor)
	}
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *httpWorld) (method, path, body string)
		want         int
		wantNotAsked bool
	}{
		{"an unknown artifact kind is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/applications/app_x/artifacts", `{"name": "api", "artifact_kind": "jar", "artifact_name": "x"}`
		}, http.StatusUnprocessableEntity, true},
		{"bad input is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "nope"}`
		}, http.StatusUnprocessableEntity, true},
		{"no lifecycle named or set", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API"}`
		}, http.StatusUnprocessableEntity, false},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			l := h.lifecycle(t, wsActive, "l", false)
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "` + l.ID() + `"}`
		}, http.StatusForbidden, false},
		{"needs approval", func(h *httpWorld) (string, string, string) {
			h.authz.outcome = authorization.OutcomeRequiresApproval
			l := h.lifecycle(t, wsActive, "l", false)
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "` + l.ID() + `"}`
		}, http.StatusForbidden, false},
		{"a missing application", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/applications/" + missingApp, ""
		}, http.StatusNotFound, true},
		{"a missing channel", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/channels/chn_01JZ0000000000000000000099/archive", ""
		}, http.StatusNotFound, true},
		{"an archived project", func(h *httpWorld) (string, string, string) {
			l := h.lifecycle(t, wsActive, "l", false)
			h.archiveProject(prjActive)
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "` + l.ID() + `"}`
		}, http.StatusConflict, false},
		{"a taken slug", func(h *httpWorld) (string, string, string) {
			l := h.lifecycle(t, wsActive, "l", false)
			h.app(t, prjActive, "api", l)
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "` + l.ID() + `"}`
		}, http.StatusConflict, false},
		{"a lifecycle in another workspace", func(h *httpWorld) (string, string, string) {
			l := h.lifecycle(t, wsOther, "l", false)
			return http.MethodPost, "/projects/" + prjActive + "/applications", `{"display_name": "API", "default_lifecycle_id": "` + l.ID() + `"}`
		}, http.StatusUnprocessableEntity, false},
		{"an inactive team", func(h *httpWorld) (string, string, string) {
			app, _ := h.app(t, prjActive, "api", h.lifecycle(t, wsActive, "l", false))
			return http.MethodPost, "/applications/" + app.ID() + "/transfer", `{"owner_team_id": "` + teamGone + `"}`
		}, http.StatusUnprocessableEntity, false},
		{"archiving the default channel", func(h *httpWorld) (string, string, string) {
			_, ch := h.app(t, prjActive, "api", h.lifecycle(t, wsActive, "l", false))
			return http.MethodPost, "/channels/" + ch.ID() + "/archive", ""
		}, http.StatusConflict, false},
		{"removing a placement in use", func(h *httpWorld) (string, string, string) {
			app, _ := h.app(t, prjActive, "api", h.lifecycle(t, wsActive, "l", false))
			tg := h.target(t, h.env(t, wsActive, "staging"), "web")
			h.placement(t, app, tg, nil)
			h.inUse[placementKey(app.ID(), tg.ID())] = true
			return http.MethodDelete, "/applications/" + app.ID() + "/placements/" + tg.ID(), ""
		}, http.StatusConflict, false},
		{"a domain rule", func(h *httpWorld) (string, string, string) {
			app, _ := h.app(t, prjActive, "api", h.lifecycle(t, wsActive, "l", false))
			return http.MethodPatch, "/applications/" + app.ID(), `{"workload_selector": {"": "x"}}`
		}, http.StatusUnprocessableEntity, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(h)
			h.authz.asked = nil
			rec, _ := h.call(t, method, path, "ada", body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.asked)
			}
		})
	}
}

func TestCreatingAnApplicationWithTheDefaultLifecycleSetting(t *testing.T) {
	h := newHTTPWorld(t)
	l := h.lifecycle(t, wsActive, "mainline", false)
	h.setDefaultLifecycle(wsActive, l.ID())

	rec, out := h.call(t, http.MethodPost, "/projects/"+prjActive+"/applications", "ada", `{"display_name": "Checkout API"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, l.ID(), data(out)["default_channel"].(map[string]any)["lifecycle_id"], "the default channel follows the workspace's default lifecycle")
}

func TestAnApplicationIsNamedByItsIDOrSlug(t *testing.T) {
	h := newHTTPWorld(t)
	checkout, _ := h.app(t, prjActive, "api", h.lifecycle(t, wsActive, "mainline", false))

	rec, out := h.call(t, http.MethodGet, "/applications/api", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, checkout.ID(), data(out)["id"])

	h.authz.asked = nil
	rec, out = h.call(t, http.MethodPost, "/applications/api/artifacts", "ada",
		`{"name": "image", "artifact_kind": "container_image", "artifact_name": "ghcr.io/acme/api"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, checkout.ID(), data(out)["application_id"], "the service gets the ID the slug names")
	require.Len(t, h.authz.asked, 1)
	assert.Equal(t, prjActive, h.authz.asked[0].ResourceID, "the decision is on the Application's Project")

	rec, out = h.call(t, http.MethodPatch, "/applications/api", "ada", `{"display_name": "API"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "API", data(out)["display_name"])

	t.Run("a slug two Projects use is refused", func(t *testing.T) {
		etl, _ := h.app(t, prjOther, "api", h.lifecycle(t, wsOther, "mainline", false))
		rec, out := h.call(t, http.MethodGet, "/applications/api", "ada", "")
		assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Contains(t, out["detail"], "use its ID")

		rec, out = h.call(t, http.MethodGet, "/applications/"+checkout.ID(), "ada", "")
		require.Equal(t, http.StatusOK, rec.Code, "the ID still works")
		assert.Equal(t, checkout.ID(), data(out)["id"])

		h.archiveApp(t, etl)
		rec, out = h.call(t, http.MethodGet, "/applications/api", "ada", "")
		require.Equal(t, http.StatusOK, rec.Code, "an archived Application's slug names nothing")
		assert.Equal(t, checkout.ID(), data(out)["id"])

		rec, _ = h.call(t, http.MethodPost, "/applications/"+etl.ID()+"/restore", "ada", "")
		assert.Equal(t, http.StatusOK, rec.Code, "an archived Application is restored by its ID")
	})

	rec, _ = h.call(t, http.MethodGet, "/applications/nothing", "ada", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

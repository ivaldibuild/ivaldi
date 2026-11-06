package teams

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
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
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
		api.WithRoutes(NewTeamController(w.svc)), api.WithVerifier(verifier{}))
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
		{http.MethodPost, "/teams"},
		{http.MethodGet, "/teams"},
		{http.MethodGet, "/teams/tem_x"},
		{http.MethodPatch, "/teams/tem_x"},
		{http.MethodPost, "/teams/tem_x/archive"},
		{http.MethodPost, "/teams/tem_x/restore"},
		{http.MethodGet, "/teams/tem_x/members"},
		{http.MethodPut, "/teams/tem_x/members/prn_x"},
		{http.MethodDelete, "/teams/tem_x/members/prn_x"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
		rec, _ = h.call(t, op.method, op.path, "forged", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with a bad token", op.method, op.path)
	}
	assert.Empty(t, h.authz.asked)
}

func TestTeamsOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodPost, "/teams", "ada", `{"display_name": "Release Engineering", "description": "Ships things"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "success", out["status"])
	data := out["data"].(map[string]any)
	id := data["id"].(string)
	assert.Equal(t, "release-engineering", data["slug"])
	assert.Equal(t, org, data["organization_id"], "the Organization comes from the token")
	require.Len(t, h.authz.asked, 1)
	assert.Equal(t, ada, h.authz.asked[0].Actor)

	rec, out = h.call(t, http.MethodPatch, "/teams/"+id, "ada", `{"display_name": "Releases"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Releases", out["data"].(map[string]any)["display_name"])

	rec, out = h.call(t, http.MethodPut, "/teams/"+id+"/members/"+alice, "ada", `{"role": "lead"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	members := out["data"].(map[string]any)["members"].([]any)
	require.Len(t, members, 1)
	assert.Equal(t, alice, members[0].(map[string]any)["principal_id"])
	assert.Equal(t, "lead", members[0].(map[string]any)["role"])

	rec, out = h.call(t, http.MethodGet, "/teams/"+id+"/members", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, out["data"].([]any), 1)

	rec, out = h.call(t, http.MethodDelete, "/teams/"+id+"/members/"+alice, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, out["data"].(map[string]any)["members"])

	rec, out = h.call(t, http.MethodGet, "/teams", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	items := out["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, id, items[0].(map[string]any)["id"])

	rec, out = h.call(t, http.MethodPost, "/teams/"+id+"/archive", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotNil(t, out["data"].(map[string]any)["archived_at"])

	rec, out = h.call(t, http.MethodPost, "/teams/"+id+"/restore", "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, out["data"].(map[string]any)["archived_at"])

	rec, _ = h.call(t, http.MethodGet, "/teams/"+id, "ada", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{
		team.EventTeamCreated, team.EventTeamUpdated, team.EventTeamMemberSet, team.EventTeamMemberRemoved,
		team.EventTeamArchived, team.EventTeamRestored,
	}, h.auditTypes())
}

func TestHTTPErrors(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(h *httpWorld) (method, path, body string)
		want         int
		wantNotAsked bool
	}{
		{"an unknown role is refused before asking", func(h *httpWorld) (string, string, string) {
			tm := h.team(t, "platform", false)
			return http.MethodPut, "/teams/" + tm.ID() + "/members/" + alice, `{"role": "owner"}`
		}, http.StatusUnprocessableEntity, true},
		{"a missing display name is refused before asking", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/teams", `{}`
		}, http.StatusUnprocessableEntity, true},
		{"a refusal", func(h *httpWorld) (string, string, string) {
			h.authz.deny = func(authorization.Request) bool { return true }
			return http.MethodPost, "/teams", `{"display_name": "Platform"}`
		}, http.StatusForbidden, false},
		{"a missing team", func(*httpWorld) (string, string, string) {
			return http.MethodGet, "/teams/tem_01JZ0000000000000000000099", ""
		}, http.StatusNotFound, false},
		{"removing someone who is not a member", func(h *httpWorld) (string, string, string) {
			tm := h.team(t, "platform", false)
			return http.MethodDelete, "/teams/" + tm.ID() + "/members/" + alice, ""
		}, http.StatusNotFound, false},
		{"a taken slug", func(h *httpWorld) (string, string, string) {
			h.team(t, "platform", false)
			return http.MethodPost, "/teams", `{"display_name": "Platform"}`
		}, http.StatusConflict, false},
		{"changing an archived team", func(h *httpWorld) (string, string, string) {
			tm := h.team(t, "platform", true)
			return http.MethodPut, "/teams/" + tm.ID() + "/members/" + alice, `{"role": "member"}`
		}, http.StatusConflict, false},
		{"a member who is not an active user", func(h *httpWorld) (string, string, string) {
			tm := h.team(t, "platform", false)
			return http.MethodPut, "/teams/" + tm.ID() + "/members/" + suspended, `{"role": "member"}`
		}, http.StatusUnprocessableEntity, false},
		{"a bad slug", func(*httpWorld) (string, string, string) {
			return http.MethodPost, "/teams", `{"display_name": "Platform", "slug": "Not A Slug"}`
		}, http.StatusUnprocessableEntity, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			method, path, body := tc.setup(h)
			rec, _ := h.call(t, method, path, "ada", body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.wantNotAsked {
				assert.Empty(t, h.authz.asked)
			}
		})
	}
}

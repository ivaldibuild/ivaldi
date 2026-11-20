package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const projectJSON = `{"id":"prj_1","organization_id":"org_1","workspace_id":"wksp_1","owner_team_id":"team_1",
	"slug":"website","display_name":"Website","description":"The public site","created_by_principal_id":"prn_ada",
	"created_at":"2026-11-06T12:00:00Z","updated_at":"2026-11-06T12:00:00Z","archived_at":%s}`

// projectsPlane fakes the Project operations, recording each request and its
// body.
func newProjectsPlane(t *testing.T) *controlPlane {
	cp := &controlPlane{}
	mux := http.NewServeMux()
	reply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var decoded map[string]any
			_ = json.NewDecoder(r.Body).Decode(&decoded)
			cp.requests = append(cp.requests, r)
			cp.bodies = append(cp.bodies, decoded)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"status":"success","data":`+body+`}`)
		}
	}
	active := strings.Replace(projectJSON, "%s", "null", 1)
	page := `{"items":[` + active + `],"total_count":1,"has_next":false,"has_previous":false,"next_cursor":null,"previous_cursor":null}`
	mux.HandleFunc("POST /v1/workspaces/{id}/projects", reply(http.StatusCreated, active))
	mux.HandleFunc("GET /v1/workspaces/{id}/projects", reply(http.StatusOK, page))
	mux.HandleFunc("GET /v1/projects/{id}", reply(http.StatusOK, active))
	mux.HandleFunc("PATCH /v1/projects/{id}", reply(http.StatusOK, active))
	mux.HandleFunc("POST /v1/projects/{id}/transfer", reply(http.StatusOK, active))
	mux.HandleFunc("POST /v1/projects/{id}/archive",
		reply(http.StatusOK, strings.Replace(projectJSON, "%s", `"2026-11-06T13:00:00Z"`, 1)))
	mux.HandleFunc("POST /v1/projects/{id}/restore", reply(http.StatusOK, active))
	cp.srv = httptest.NewServer(mux)
	t.Cleanup(cp.srv.Close)
	return cp
}

func TestProjects(t *testing.T) {
	cp := newProjectsPlane(t)
	store := memoryStore{cp.srv.URL: {AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: now.Add(time.Hour)}}

	t.Run("create", func(t *testing.T) {
		out, err := run(t, cp, nil, store,
			"project", "create", "--workspace", "wksp_1", "--name", "Website", "--slug", "website",
			"--team", "team_1", "--description", "The public site", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/workspaces/wksp_1/projects" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := cp.bodies[len(cp.bodies)-1]
		if body["slug"] != "website" || body["display_name"] != "Website" ||
			body["owner_team_id"] != "team_1" || body["description"] != "The public site" {
			t.Errorf("body %v", body)
		}
		if !strings.Contains(out, "Project Website (prj_1) is active.") {
			t.Errorf("text output %q", out)
		}
	})

	t.Run("create without a description sends none", func(t *testing.T) {
		_, err := run(t, cp, nil, store,
			"project", "create", "--workspace", "wksp_1", "--name", "Website", "--slug", "website", "--team", "team_1")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := cp.bodies[len(cp.bodies)-1]["description"]; ok {
			t.Errorf("body %v", cp.bodies[len(cp.bodies)-1])
		}
	})

	t.Run("list", func(t *testing.T) {
		out, err := run(t, cp, nil, store,
			"project", "list", "--workspace", "wksp_1", "-q", "web", "--all", "--sort", "slug", "--order", "asc", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		q := cp.requests[len(cp.requests)-1].URL.Query()
		if q.Get("q") != "web" || q.Get("include_archived") != "true" ||
			q.Get("sort_by") != "slug" || q.Get("sort_order") != "asc" {
			t.Errorf("query %s", q.Encode())
		}
		if !strings.Contains(out, "prj_1") || !strings.Contains(out, "1 of 1") {
			t.Errorf("text output %q", out)
		}
	})

	t.Run("list rejects a bad sort", func(t *testing.T) {
		sent := len(cp.requests)
		_, err := run(t, cp, nil, store, "project", "list", "--workspace", "wksp_1", "--sort", "size")
		if err == nil || !strings.Contains(err.Error(), "must be created_at, slug, or display_name") {
			t.Errorf("got %v", err)
		}
		if len(cp.requests) != sent {
			t.Error("nothing was sent")
		}
	})

	t.Run("get", func(t *testing.T) {
		out, err := run(t, cp, nil, store, "project", "get", "prj_1")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/projects/prj_1" {
			t.Errorf("path %s", p)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil || got["id"] != "prj_1" || got["slug"] != "website" {
			t.Errorf("JSON output %q (%v)", out, err)
		}
	})

	t.Run("update sends only what changed", func(t *testing.T) {
		_, err := run(t, cp, nil, store, "project", "update", "prj_1", "--name", "Web")
		if err != nil {
			t.Fatal(err)
		}
		body := cp.bodies[len(cp.bodies)-1]
		if body["display_name"] != "Web" {
			t.Errorf("body %v", body)
		}
		if _, ok := body["slug"]; ok {
			t.Errorf("slug was not given, so none is sent: %v", body)
		}

		sent := len(cp.requests)
		if _, err := run(t, cp, nil, store, "project", "update", "prj_1"); err == nil ||
			!strings.Contains(err.Error(), "give --name, --slug, or --description") {
			t.Errorf("nothing to change is an error: %v", err)
		}
		if len(cp.requests) != sent {
			t.Error("nothing was sent")
		}
	})

	t.Run("transfer", func(t *testing.T) {
		_, err := run(t, cp, nil, store, "project", "transfer", "prj_1", "--team", "team_2")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/projects/prj_1/transfer" {
			t.Errorf("path %s", r.URL.Path)
		}
		if body := cp.bodies[len(cp.bodies)-1]; body["owner_team_id"] != "team_2" {
			t.Errorf("body %v", body)
		}
	})

	t.Run("archive and restore", func(t *testing.T) {
		out, err := run(t, cp, nil, store, "project", "archive", "prj_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/projects/prj_1/archive" {
			t.Errorf("path %s", p)
		}
		if !strings.Contains(out, "is archived") {
			t.Errorf("text output %q", out)
		}

		out, err = run(t, cp, nil, store, "project", "restore", "prj_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/projects/prj_1/restore" {
			t.Errorf("path %s", p)
		}
		if !strings.Contains(out, "is active") {
			t.Errorf("text output %q", out)
		}
	})
}

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

const applicationJSON = `{"id":"app_1","organization_id":"org_1","workspace_id":"wksp_1","project_id":"prj_1",
	"owner_team_id":"team_1","slug":"payments","display_name":"Payments","source_id":%s,
	"workload_selector":{"app":"payments"},
	"created_at":"2026-11-06T12:00:00Z","updated_at":"2026-11-06T12:00:00Z","archived_at":%s}`

// applicationsPlane fakes the Application operations, recording each request
// and its body.
func newApplicationsPlane(t *testing.T) *controlPlane {
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
	active := strings.Replace(strings.Replace(applicationJSON, "%s", `"src_1"`, 1), "%s", "null", 1)
	page := `{"items":[` + active + `],"total_count":1,"has_next":false,"has_previous":false,"next_cursor":null,"previous_cursor":null}`
	created := `{"application":` + active + `,"default_channel":{"id":"chn_1","organization_id":"org_1",
		"application_id":"app_1","slug":"production","display_name":"Production","description":null,
		"lifecycle_id":"lfc_1","is_default":true,"rules":[],
		"created_at":"2026-11-06T12:00:00Z","updated_at":"2026-11-06T12:00:00Z","archived_at":null}}`
	mux.HandleFunc("POST /v1/projects/{id}/applications", reply(http.StatusCreated, created))
	mux.HandleFunc("GET /v1/projects/{id}/applications", reply(http.StatusOK, page))
	mux.HandleFunc("GET /v1/workspaces/{id}/applications", reply(http.StatusOK, page))
	mux.HandleFunc("GET /v1/applications/{id}", reply(http.StatusOK, active))
	mux.HandleFunc("PATCH /v1/applications/{id}", reply(http.StatusOK, active))
	mux.HandleFunc("PUT /v1/applications/{id}/source", reply(http.StatusOK, active))
	mux.HandleFunc("POST /v1/applications/{id}/transfer", reply(http.StatusOK, active))
	mux.HandleFunc("POST /v1/applications/{id}/archive", reply(http.StatusOK,
		strings.Replace(strings.Replace(applicationJSON, "%s", `"src_1"`, 1), "%s", `"2026-11-06T13:00:00Z"`, 1)))
	mux.HandleFunc("POST /v1/applications/{id}/restore", reply(http.StatusOK, active))
	cp.srv = httptest.NewServer(mux)
	t.Cleanup(cp.srv.Close)
	return cp
}

func TestApplications(t *testing.T) {
	cp := newApplicationsPlane(t)
	store := memoryStore{cp.srv.URL: {AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: now.Add(time.Hour)}}

	t.Run("create", func(t *testing.T) {
		out, err := run(t, cp, nil, store,
			"application", "create", "--project", "prj_1", "--name", "Payments", "--source", "src_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/projects/prj_1/applications" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := cp.bodies[len(cp.bodies)-1]
		if body["display_name"] != "Payments" || body["source_id"] != "src_1" {
			t.Errorf("body %v", body)
		}
		if _, ok := body["slug"]; ok {
			t.Errorf("no slug was given, so none is sent: %v", body)
		}
		if !strings.Contains(out, "Application Payments (app_1) is active, with default Channel Production (chn_1).") {
			t.Errorf("text output %q", out)
		}
	})

	t.Run("list in a Project and in a Workspace", func(t *testing.T) {
		_, err := run(t, cp, nil, store, "app", "list", "--project", "prj_1", "--team", "team_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/projects/prj_1/applications" || r.URL.Query().Get("owner_team_id") != "team_1" {
			t.Errorf("path %s, query %s", r.URL.Path, r.URL.RawQuery)
		}

		out, err := run(t, cp, nil, store, "app", "list", "--workspace", "wksp_1", "--all",
			"--sort", "slug", "--order", "desc", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r = cp.requests[len(cp.requests)-1]
		q := r.URL.Query()
		if r.URL.Path != "/v1/workspaces/wksp_1/applications" || q.Get("include_archived") != "true" ||
			q.Get("sort_by") != "slug" || q.Get("sort_order") != "desc" {
			t.Errorf("path %s, query %s", r.URL.Path, r.URL.RawQuery)
		}
		if !strings.Contains(out, "app_1") || !strings.Contains(out, "1 of 1") {
			t.Errorf("text output %q", out)
		}

		sent := len(cp.requests)
		if _, err := run(t, cp, nil, store, "app", "list"); err == nil ||
			!strings.Contains(err.Error(), "exactly one of --project or --workspace") {
			t.Errorf("neither scope is an error: %v", err)
		}
		if _, err := run(t, cp, nil, store, "app", "list", "--project", "prj_1", "--workspace", "wksp_1"); err == nil {
			t.Error("both scopes is an error")
		}
		if _, err := run(t, cp, nil, store, "app", "list", "--project", "prj_1", "--order", "up"); err == nil ||
			!strings.Contains(err.Error(), "must be asc or desc") {
			t.Errorf("a bad order is an error: %v", err)
		}
		if len(cp.requests) != sent {
			t.Error("nothing was sent")
		}
	})

	t.Run("get by slug", func(t *testing.T) {
		out, err := run(t, cp, nil, store, "application", "get", "payments")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/applications/payments" {
			t.Errorf("path %s", p)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil || got["id"] != "app_1" || got["project_id"] != "prj_1" {
			t.Errorf("JSON output %q (%v)", out, err)
		}
	})

	t.Run("update sends only what changed", func(t *testing.T) {
		_, err := run(t, cp, nil, store, "application", "update", "app_1", "--slug", "payments-api")
		if err != nil {
			t.Fatal(err)
		}
		body := cp.bodies[len(cp.bodies)-1]
		if body["slug"] != "payments-api" {
			t.Errorf("body %v", body)
		}
		if _, ok := body["display_name"]; ok {
			t.Errorf("no name was given, so none is sent: %v", body)
		}

		sent := len(cp.requests)
		if _, err := run(t, cp, nil, store, "application", "update", "app_1"); err == nil ||
			!strings.Contains(err.Error(), "give --name or --slug") {
			t.Errorf("nothing to change is an error: %v", err)
		}
		if len(cp.requests) != sent {
			t.Error("nothing was sent")
		}
	})

	t.Run("link-source and unlink", func(t *testing.T) {
		out, err := run(t, cp, nil, store, "application", "link-source", "app_1", "--source", "src_2", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.Method != http.MethodPut || r.URL.Path != "/v1/applications/app_1/source" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if body := cp.bodies[len(cp.bodies)-1]; body["source_id"] != "src_2" {
			t.Errorf("body %v", body)
		}
		if !strings.Contains(out, "Built from Source src_1.") {
			t.Errorf("text output %q", out)
		}

		if _, err := run(t, cp, nil, store, "application", "link-source", "app_1", "--source", ""); err != nil {
			t.Fatal(err)
		}
		if body := cp.bodies[len(cp.bodies)-1]; body["source_id"] != "" {
			t.Errorf("an empty source unlinks: %v", body)
		}
	})

	t.Run("transfer", func(t *testing.T) {
		_, err := run(t, cp, nil, store, "application", "transfer", "app_1", "--team", "team_2")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/applications/app_1/transfer" {
			t.Errorf("path %s", r.URL.Path)
		}
		if body := cp.bodies[len(cp.bodies)-1]; body["owner_team_id"] != "team_2" {
			t.Errorf("body %v", body)
		}
	})

	t.Run("archive and restore", func(t *testing.T) {
		out, err := run(t, cp, nil, store, "application", "archive", "app_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/applications/app_1/archive" {
			t.Errorf("path %s", p)
		}
		if !strings.Contains(out, "is archived") {
			t.Errorf("text output %q", out)
		}

		out, err = run(t, cp, nil, store, "application", "restore", "app_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/applications/app_1/restore" {
			t.Errorf("path %s", p)
		}
		if !strings.Contains(out, "is active") {
			t.Errorf("text output %q", out)
		}
	})
}

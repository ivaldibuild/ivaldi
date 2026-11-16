package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
	"github.com/ivaldibuild/ivaldi-cli/internal/credentials"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// memoryStore keeps sessions in memory.
type memoryStore map[string]credentials.Session

func (m memoryStore) Load(server string) (credentials.Session, error) {
	s, ok := m[server]
	if !ok {
		return credentials.Session{}, credentials.ErrNoSession
	}
	return s, nil
}
func (m memoryStore) Save(server string, s credentials.Session) error { m[server] = s; return nil }
func (m memoryStore) Delete(server string) error                      { delete(m, server); return nil }

// controlPlane fakes the operations ivaldictl calls, recording requests.
type controlPlane struct {
	srv      *httptest.Server
	requests []*http.Request
	bodies   []map[string]any
	status   int
	reply    string
}

func newControlPlane(t *testing.T) *controlPlane {
	cp := &controlPlane{status: http.StatusCreated}
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		cp.requests = append(cp.requests, r)
		cp.bodies = append(cp.bodies, body)
	}
	mux.HandleFunc("POST /v1/applications/{id}/releases", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if strings.HasPrefix(cp.reply, "<") {
			w.Header().Set("Content-Type", "text/html")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(cp.status)
		if cp.reply != "" {
			_, _ = io.WriteString(w, cp.reply)
			return
		}
		_, _ = io.WriteString(w, `{"status":"success","data":{"id":"rel_1","application_id":"app_1","channel_id":"chn_main",
			"version":"1.4.0","status":"published","builds":[{"artifact_id":"art_api","build":{"artifact_name":"ghcr.io/acme/api","digest":"sha256:aa"}}]}}`)
	})
	mux.HandleFunc("POST /v1/sources/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"success","data":{"id":"chg_1","organization_id":"org_1","source_id":"src_1",
			"title":"Add checkout","commit_shas":["`+strings.Repeat("a", 40)+`"],"created_at":"2026-11-06T12:00:00Z","updated_at":"2026-11-06T12:00:00Z"}}`)
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":{"principal":{"id":"prn_1","kind":"service_account","display_name":"GitHub Actions"},
			"organization":{"id":"org_1","slug":"acme","display_name":"Acme"},"roles":[]}}`)
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt-1" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"at-2","refresh_token":"rt-2","expires_in":900}`)
	})
	cp.srv = httptest.NewServer(mux)
	t.Cleanup(cp.srv.Close)
	return cp
}

func run(t *testing.T, cp *controlPlane, vars map[string]string, store memoryStore, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	a := &app.App{
		Getenv: func(k string) string { return vars[k] },
		Now:    func() time.Time { return now },
		HTTP:   cp.srv.Client(),
		Store:  store,
		Out:    &out,
		OpenBrowser: func(string) error {
			t.Error("no browser should open")
			return nil
		},
	}
	cmd := New(a)
	cmd.SetArgs(append([]string{"--server-url", cp.srv.URL}, args...))
	cmd.SetErr(io.Discard)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// githubTag is GitHub Actions' environment on a v1.4.0 tag, with a service
// account's token.
func githubTag() map[string]string {
	return map[string]string{
		"CI": "true", "GITHUB_ACTIONS": "true", "GITHUB_SHA": strings.Repeat("a", 40), "GITHUB_REF": "refs/tags/v1.4.0",
		"GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "acme/api", "GITHUB_RUN_ID": "77",
		"GITHUB_ACTOR": "ada", "IVALDI_API_TOKEN": "ivt_from_env",
	}
}

func TestReleaseCreateFromATagInCI(t *testing.T) {
	cp := newControlPlane(t)
	out, err := run(t, cp, githubTag(), memoryStore{}, "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa", "--publish", "-o", "text")
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.requests) != 1 {
		t.Fatalf("%d requests", len(cp.requests))
	}
	if got := cp.requests[0].Header.Get("Authorization"); got != "Bearer ivt_from_env" {
		t.Errorf("authorization %q", got)
	}
	if cp.requests[0].URL.Path != "/v1/applications/app_1/releases" {
		t.Errorf("path %s", cp.requests[0].URL.Path)
	}
	body := cp.bodies[0]
	if body["version"] != "1.4.0" || body["publish"] != true {
		t.Errorf("the version comes from the tag: %v", body)
	}
	if body["triggered_by"] != "ada" {
		t.Errorf("who triggered it comes from CI: %v", body["triggered_by"])
	}
	if _, ok := body["triggered_by_email"]; ok {
		t.Errorf("GitHub Actions exposes no email, so none is sent: %v", body["triggered_by_email"])
	}
	build := body["builds"].([]any)[0].(map[string]any)
	want := map[string]any{
		"artifact": "api", "digest": "sha256:aa", "version": "1.4.0", "commit_sha": strings.Repeat("a", 40),
		"source_ref": "refs/tags/v1.4.0", "ci_provider": "github_actions", "ci_run_ref": "https://github.com/acme/api/actions/runs/77",
	}
	for k, v := range want {
		if build[k] != v {
			t.Errorf("build %s = %v, want %v", k, build[k], v)
		}
	}
	if !strings.Contains(out, "Release 1.4.0 (rel_1) is published") || !strings.Contains(out, "ghcr.io/acme/api@sha256:aa") {
		t.Errorf("text output %q", out)
	}
}

func TestReleaseCreateTriggeredByFlagsWinOverCI(t *testing.T) {
	cp := newControlPlane(t)
	_, err := run(t, cp, githubTag(), memoryStore{}, "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa",
		"--triggered-by-email", "grace@example.com")
	if err != nil {
		t.Fatal(err)
	}
	body := cp.bodies[0]
	if body["triggered_by_email"] != "grace@example.com" {
		t.Errorf("triggered_by_email = %v", body["triggered_by_email"])
	}
	if _, ok := body["triggered_by"]; ok {
		t.Errorf("a flag replaces both of CI's values, so CI's name is not paired with another email: %v", body["triggered_by"])
	}
}

func TestChangeRecordTakesTheCommitFromCI(t *testing.T) {
	cp := newControlPlane(t)
	out, err := run(t, cp, githubTag(), memoryStore{}, "change", "record", "--source", "src_1", "--title", " Add checkout ",
		"--work-item", "wki_1", "--external-ref", "https://github.com/acme/api/pull/42", "-o", "text")
	if err != nil {
		t.Fatal(err)
	}
	if cp.requests[0].URL.Path != "/v1/sources/src_1/changes" {
		t.Errorf("path %s", cp.requests[0].URL.Path)
	}
	body := cp.bodies[0]
	if commits := body["commit_shas"].([]any); len(commits) != 1 || commits[0] != strings.Repeat("a", 40) {
		t.Errorf("the commit comes from CI: %v", commits)
	}
	if body["title"] != "Add checkout" || body["work_item_id"] != "wki_1" || body["external_ref"] != "https://github.com/acme/api/pull/42" {
		t.Errorf("body %v", body)
	}
	if _, ok := body["session_id"]; ok {
		t.Errorf("no Session was given, so none is sent: %v", body["session_id"])
	}
	if !strings.Contains(out, "Change Add checkout (chg_1) holds 1 commit(s)") {
		t.Errorf("text output %q", out)
	}

	_, err = run(t, cp, map[string]string{"IVALDI_API_TOKEN": "ivt_x"}, memoryStore{}, "change", "record", "--source", "src_1", "--title", "Fix")
	if err == nil || !strings.Contains(err.Error(), "give --commit") {
		t.Errorf("outside CI a commit is required: %v", err)
	}
}

func TestAPITokenFlagWinsOverTheEnvironment(t *testing.T) {
	cp := newControlPlane(t)
	if _, err := run(t, cp, githubTag(), memoryStore{}, "--api-token", "ivt_flag", "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa"); err != nil {
		t.Fatal(err)
	}
	if got := cp.requests[0].Header.Get("Authorization"); got != "Bearer ivt_flag" {
		t.Errorf("authorization %q", got)
	}
}

func TestCIWithoutCredentialsFailsWithoutWaitingForABrowser(t *testing.T) {
	cp := newControlPlane(t)
	vars := githubTag()
	delete(vars, "IVALDI_API_TOKEN")
	_, err := run(t, cp, vars, memoryStore{}, "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa")
	if !errors.Is(err, app.ErrNoCredentials) {
		t.Errorf("got %v", err)
	}
	_, err = run(t, cp, vars, memoryStore{}, "auth", "login")
	if err == nil || !strings.Contains(err.Error(), "IVALDI_API_TOKEN") {
		t.Errorf("login in CI is refused: %v", err)
	}
	if len(cp.requests) != 0 {
		t.Error("nothing reached the control plane")
	}
}

func TestASignedInSessionIsRefreshedWhenItExpires(t *testing.T) {
	cp := newControlPlane(t)
	store := memoryStore{cp.srv.URL: {AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: now.Add(10 * time.Second)}}
	if _, err := run(t, cp, map[string]string{}, store, "auth", "status"); err != nil {
		t.Fatal(err)
	}
	if got := cp.requests[0].Header.Get("Authorization"); got != "Bearer at-2" {
		t.Errorf("an access token about to expire is refreshed first: %q", got)
	}
	if store[cp.srv.URL].RefreshToken != "rt-2" {
		t.Error("the rotated refresh token is kept")
	}

	t.Run("a session that cannot be refreshed is dropped", func(t *testing.T) {
		store := memoryStore{cp.srv.URL: {AccessToken: "at-old", RefreshToken: "rt-used", ExpiresAt: now.Add(-time.Hour)}}
		_, err := run(t, cp, map[string]string{}, store, "auth", "status")
		if err == nil || !strings.Contains(err.Error(), "auth login") {
			t.Errorf("got %v", err)
		}
		if _, ok := store[cp.srv.URL]; ok {
			t.Error("the dead session is removed")
		}
	})
}

func TestNotSignedIn(t *testing.T) {
	cp := newControlPlane(t)
	_, err := run(t, cp, map[string]string{}, memoryStore{}, "auth", "status")
	if err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("got %v", err)
	}
}

func TestReleaseCreateInputErrors(t *testing.T) {
	pr := githubTag()
	pr["GITHUB_REF"] = "refs/pull/42/merge"
	cases := map[string]struct {
		vars map[string]string
		args []string
		want string
	}{
		"a pull request needs a version": {pr, []string{"--artifact", "api=sha256:aa"}, "give --version"},
		"a tag instead of a digest":      {githubTag(), []string{"--artifact", "api=v1.4.0"}, "is not a digest"},
		"no name":                        {githubTag(), []string{"--artifact", "sha256:aa"}, "must be name=digest"},
		"an artifact twice":              {githubTag(), []string{"--artifact", "api=sha256:aa", "--artifact", "api=sha256:bb"}, "given twice"},
		"a short ref":                    {githubTag(), []string{"--artifact", "api=sha256:aa", "--ref", "main"}, "full Git ref"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cp := newControlPlane(t)
			_, err := run(t, cp, tc.vars, memoryStore{}, append([]string{"release", "create", "--app", "app_1"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			if len(cp.requests) != 0 {
				t.Error("nothing was sent")
			}
		})
	}

	t.Run("a pull request with a version is a draft", func(t *testing.T) {
		cp := newControlPlane(t)
		if _, err := run(t, cp, pr, memoryStore{}, "release", "create", "--app", "app_1", "--version", "1.5.0-pr.42", "--artifact", "api=sha256:aa"); err != nil {
			t.Fatal(err)
		}
		body := cp.bodies[0]
		if body["version"] != "1.5.0-pr.42" || body["publish"] != nil {
			t.Errorf("body %v", body)
		}
		if ref := body["builds"].([]any)[0].(map[string]any)["source_ref"]; ref != "refs/pull/42/merge" {
			t.Errorf("ref %v", ref)
		}
	})
}

func TestTheControlPlanesRefusalIsShown(t *testing.T) {
	cp := newControlPlane(t)
	cp.status = http.StatusUnprocessableEntity
	cp.reply = `{"title":"Unprocessable Entity","status":422,"detail":"no channel's rules admit the release"}`
	_, err := run(t, cp, githubTag(), memoryStore{}, "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa")
	if err == nil || err.Error() != "no channel's rules admit the release" {
		t.Errorf("got %v", err)
	}
}

func TestAnAnswerTheCLICannotReadIsAnError(t *testing.T) {
	cp := newControlPlane(t)
	cp.reply = "<html>a proxy's page</html>"
	_, err := run(t, cp, githubTag(), memoryStore{}, "release", "create", "--app", "app_1", "--artifact", "api=sha256:aa")
	if err == nil || !strings.Contains(err.Error(), "could not read") {
		t.Errorf("got %v", err)
	}
}

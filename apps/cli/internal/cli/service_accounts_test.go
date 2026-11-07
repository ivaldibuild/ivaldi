package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivaldibuild/ivaldi-cli/internal/app"
)

const serviceAccountJSON = `{"id":"prn_ci","display_name":"GitHub Actions","description":"Releases from CI","status":"%s",
	"created_by_principal_id":"prn_ada","created_at":"2026-11-06T12:00:00Z","updated_at":"2026-11-06T12:00:00Z"}`

const tokenJSON = `{"id":"tok_1","organization_id":"org_1","principal_id":"prn_ci","name":"github-actions","token_prefix":"ivt_abcdefgh",
	"expires_at":%s,"last_used_at":null,"revoked_at":%s,"created_at":"2026-11-06T12:00:00Z"}`

// accountsPlane fakes the service account and API token operations,
// recording each request and its body.
type accountsPlane struct {
	*controlPlane
}

func newAccountsPlane(t *testing.T) *accountsPlane {
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
	active := strings.Replace(serviceAccountJSON, "%s", "active", 1)
	mux.HandleFunc("POST /v1/service-accounts", reply(http.StatusCreated, active))
	mux.HandleFunc("GET /v1/service-accounts", reply(http.StatusOK, "["+active+"]"))
	mux.HandleFunc("GET /v1/service-accounts/{id}", reply(http.StatusOK, active))
	mux.HandleFunc("POST /v1/service-accounts/{id}/suspend", reply(http.StatusOK, strings.Replace(serviceAccountJSON, "%s", "suspended", 1)))
	mux.HandleFunc("POST /v1/service-accounts/{id}/tokens", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		expires := "null"
		if v, ok := body["expires_at"].(string); ok {
			expires = `"` + v + `"`
		}
		r.Body = io.NopCloser(strings.NewReader(""))
		cp.requests = append(cp.requests, r)
		cp.bodies = append(cp.bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"success","data":{"secret":"ivt_secret","token":`+
			strings.Replace(strings.Replace(tokenJSON, "%s", expires, 1), "%s", "null", 1)+`}}`)
	})
	mux.HandleFunc("GET /v1/service-accounts/{id}/tokens", reply(http.StatusOK,
		"["+strings.Replace(strings.Replace(tokenJSON, "%s", "null", 1), "%s", "null", 1)+"]"))
	mux.HandleFunc("POST /v1/api-tokens/{id}/revoke", reply(http.StatusOK,
		strings.Replace(strings.Replace(tokenJSON, "%s", "null", 1), "%s", `"2026-11-06T13:00:00Z"`, 1)))
	cp.srv = httptest.NewServer(mux)
	t.Cleanup(cp.srv.Close)
	return &accountsPlane{cp}
}

// signedIn is a person's live session for the fake control plane.
func signedIn(cp *accountsPlane) memoryStore {
	return memoryStore{cp.srv.URL: {AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: now.Add(time.Hour)}}
}

// runCapturing is run that also returns what was written to stderr.
func runCapturing(t *testing.T, cp *controlPlane, store memoryStore, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &app.App{
		Getenv: func(string) string { return "" },
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
	cmd.SetErr(&errOut)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestServiceAccounts(t *testing.T) {
	cp := newAccountsPlane(t)
	store := signedIn(cp)

	out, err := run(t, cp.controlPlane, nil, store, "service-account", "create", "--name", "GitHub Actions", "--description", "Releases from CI", "-o", "text")
	if err != nil {
		t.Fatal(err)
	}
	if got := cp.bodies[0]; got["display_name"] != "GitHub Actions" || got["description"] != "Releases from CI" {
		t.Errorf("body %v", got)
	}
	if got := cp.requests[0].Header.Get("Authorization"); got != "Bearer at-1" {
		t.Errorf("a person's session authenticates: %q", got)
	}
	if !strings.Contains(out, "Service account GitHub Actions (prn_ci) is active.") {
		t.Errorf("text output %q", out)
	}

	out, err = run(t, cp.controlPlane, nil, store, "sa", "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if q := cp.requests[1].URL.Query().Get("include_inactive"); q != "true" {
		t.Errorf("--all lists suspended accounts too: include_inactive=%q", q)
	}
	var listed []map[string]any
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed) != 1 || listed[0]["id"] != "prn_ci" {
		t.Errorf("JSON output %q (%v)", out, err)
	}

	if _, err := run(t, cp.controlPlane, nil, store, "sa", "list"); err != nil {
		t.Fatal(err)
	}
	if cp.requests[2].URL.RawQuery != "" {
		t.Errorf("without --all, no query: %q", cp.requests[2].URL.RawQuery)
	}

	out, err = run(t, cp.controlPlane, nil, store, "service-account", "suspend", "prn_ci", "-o", "text")
	if err != nil {
		t.Fatal(err)
	}
	if cp.requests[3].URL.Path != "/v1/service-accounts/prn_ci/suspend" || !strings.Contains(out, "is suspended") {
		t.Errorf("path %s, output %q", cp.requests[3].URL.Path, out)
	}
}

func TestAPITokens(t *testing.T) {
	cp := newAccountsPlane(t)
	store := signedIn(cp)

	t.Run("the secret is shown once", func(t *testing.T) {
		out, errOut, err := runCapturing(t, cp.controlPlane, store,
			"api-token", "create", "--service-account", "prn_ci", "--name", "github-actions", "--expires-in", "90d", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		r := cp.requests[len(cp.requests)-1]
		if r.URL.Path != "/v1/service-accounts/prn_ci/tokens" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := cp.bodies[len(cp.bodies)-1]
		if body["name"] != "github-actions" || body["expires_at"] != "2027-02-04T12:00:00Z" {
			t.Errorf("90 days from the clock: %v", body)
		}
		if !strings.Contains(out, "ivt_secret") || !strings.Contains(out, "expires 2027-02-04T12:00:00Z") {
			t.Errorf("text output %q", out)
		}
		if !strings.Contains(errOut, "will not be shown again") {
			t.Errorf("stderr %q", errOut)
		}
	})

	t.Run("JSON carries the whole issued token", func(t *testing.T) {
		out, _, err := runCapturing(t, cp.controlPlane, store,
			"api-token", "create", "--service-account", "prn_ci", "--name", "github-actions", "--expires-at", "2027-06-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if body := cp.bodies[len(cp.bodies)-1]; body["expires_at"] != "2027-06-01T00:00:00Z" {
			t.Errorf("body %v", body)
		}
		var issued map[string]any
		if err := json.Unmarshal([]byte(out), &issued); err != nil || issued["secret"] != "ivt_secret" || issued["token"].(map[string]any)["id"] != "tok_1" {
			t.Errorf("JSON output %q (%v)", out, err)
		}
	})

	t.Run("lists and revokes", func(t *testing.T) {
		out, err := run(t, cp.controlPlane, nil, store, "api-token", "list", "--service-account", "prn_ci", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "ivt_abcdefgh") || !strings.Contains(out, "never expires") || strings.Contains(out, "ivt_secret") {
			t.Errorf("text output %q", out)
		}
		out, err = run(t, cp.controlPlane, nil, store, "api-token", "revoke", "tok_1", "-o", "text")
		if err != nil {
			t.Fatal(err)
		}
		if p := cp.requests[len(cp.requests)-1].URL.Path; p != "/v1/api-tokens/tok_1/revoke" {
			t.Errorf("path %s", p)
		}
		if !strings.Contains(out, "is revoked 2026-11-06T13:00:00Z") {
			t.Errorf("text output %q", out)
		}
	})

	t.Run("input errors send nothing", func(t *testing.T) {
		sent := len(cp.requests)
		for _, tc := range []struct {
			args []string
			want string
		}{
			{[]string{"--expires-at", "2027-06-01T00:00:00Z", "--expires-in", "90d"}, "none of the others can be"},
			{[]string{"--expires-in", "ninety days"}, "must be days"},
			{[]string{"--expires-in", "-1h"}, "in the future"},
			{[]string{"--expires-at", "2027-06-01"}, "RFC 3339"},
		} {
			_, err := run(t, cp.controlPlane, nil, store, append([]string{"api-token", "create", "--service-account", "prn_ci", "--name", "x"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%v: got %v, want %q", tc.args, err, tc.want)
			}
		}
		if len(cp.requests) != sent {
			t.Error("nothing was sent")
		}
	})
}

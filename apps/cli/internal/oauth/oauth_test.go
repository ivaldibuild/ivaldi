package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// fakeServer is a control plane's /oauth/token and /oauth/revoke. It checks
// the PKCE verifier against the challenge the authorize URL carried.
type fakeServer struct {
	challenge string
	revoked   []string
	refreshes int
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != ClientID {
			t.Errorf("client_id %q", r.Form.Get("client_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("code") != "the-code" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":900,"token_type":"Bearer"}`)
		case "refresh_token":
			f.refreshes++
			if r.Form.Get("refresh_token") != "rt-1" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"at-2","refresh_token":"rt-2","expires_in":900}`)
		}
	})
	mux.HandleFunc("POST /oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.revoked = append(f.revoked, r.Form.Get("token"))
	})
	return mux
}

// browser follows the authorize URL as a signed-in person would: the
// control plane redirects back with a code.
func browser(t *testing.T, f *fakeServer, query url.Values) func(string) error {
	return func(authorize string) error {
		u, err := url.Parse(authorize)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != ClientID {
			t.Errorf("authorize query %v", q)
		}
		f.challenge = q.Get("code_challenge")
		back, _ := url.Parse(q.Get("redirect_uri"))
		if back.Hostname() != "127.0.0.1" || back.Path != "/callback" {
			t.Errorf("redirect %s is not ivaldictl's loopback callback", back)
		}
		reply := url.Values{"state": {q.Get("state")}, "code": {"the-code"}}
		for k, v := range query {
			reply[k] = v
		}
		back.RawQuery = reply.Encode()
		go func() {
			resp, err := http.Get(back.String())
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin(t *testing.T) {
	f := &fakeServer{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	var out strings.Builder

	s, err := Flow{
		Server: srv.URL, HTTP: srv.Client(), Organization: "acme",
		OpenBrowser: browser(t, f, nil), Out: &out, Timeout: 5 * time.Second, Now: func() time.Time { return now },
	}.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.AccessToken != "at-1" || s.RefreshToken != "rt-1" || !s.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Errorf("session %+v", s)
	}
	if !strings.Contains(out.String(), "/oauth/authorize?") || !strings.Contains(out.String(), "organization=acme") {
		t.Errorf("the sign-in address is printed for opening by hand: %q", out.String())
	}
}

func TestLoginRefusals(t *testing.T) {
	cases := map[string]url.Values{
		"the person cancelled":    {"error": {"access_denied"}, "code": {""}},
		"another sign-in's state": {"state": {"not-ours"}},
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeServer{}
			srv := httptest.NewServer(f.handler(t))
			defer srv.Close()
			_, err := Flow{
				Server: srv.URL, HTTP: srv.Client(), OpenBrowser: browser(t, f, reply),
				Out: io.Discard, Timeout: 500 * time.Millisecond,
			}.Login(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRefreshAndRevoke(t *testing.T) {
	f := &fakeServer{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	s, err := Refresh(context.Background(), srv.Client(), srv.URL, "rt-1", func() time.Time { return now })
	if err != nil || s.AccessToken != "at-2" || s.RefreshToken != "rt-2" {
		t.Fatalf("refresh: %+v %v", s, err)
	}
	if _, err := Refresh(context.Background(), srv.Client(), srv.URL, "rt-old", time.Now); err != ErrSignInAgain {
		t.Errorf("a used refresh token means signing in again, got %v", err)
	}
	if err := Revoke(context.Background(), srv.Client(), srv.URL+"/", "rt-2"); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(f.revoked); string(b) != `["rt-2"]` {
		t.Errorf("revoked %s", b)
	}
}

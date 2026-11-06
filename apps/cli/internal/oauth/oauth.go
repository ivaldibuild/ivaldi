// Package oauth signs ivaldictl in to a control plane as its first-party
// OAuth client: the authorization code flow with PKCE and a loopback
// redirect (RFC 8252), then refreshing and revoking the tokens it got.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-cli/internal/credentials"
)

// ClientID is ivaldictl's client ID, which every control plane registers
// with loopback redirects.
const ClientID = "ivaldictl"

const callbackPath = "/callback"

// Flow signs a person in through their browser.
type Flow struct {
	// Server is the control plane's URL, such as https://ivaldi.example.com.
	Server string
	HTTP   *http.Client
	// Organization is the slug or ID to sign in to, when the person belongs
	// to several; empty lets them choose.
	Organization string
	// OpenBrowser opens the sign-in page. The page's URL is also written to
	// Out, so a person can open it themselves when this fails.
	OpenBrowser func(url string) error
	Out         io.Writer
	// Timeout bounds the wait for the browser to come back.
	Timeout time.Duration
	// Now is the clock, for the session's expiry.
	Now func() time.Time
}

// Login runs the flow: it listens on a loopback port, sends the browser to
// the authorization endpoint, waits for the redirect with the code, and
// exchanges it for tokens.
func (f Flow) Login(ctx context.Context) (credentials.Session, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return credentials.Session{}, fmt.Errorf("listen for the sign-in redirect: %w", err)
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://%s%s", listener.Addr().String(), callbackPath)

	verifier, challenge, err := newPKCE()
	if err != nil {
		return credentials.Session{}, err
	}
	state, err := randomString(16)
	if err != nil {
		return credentials.Session{}, err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {redirect},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if f.Organization != "" {
		q.Set("organization", f.Organization)
	}
	authorize := strings.TrimRight(f.Server, "/") + "/oauth/authorize?" + q.Encode()

	codes := make(chan result, 1)
	server := &http.Server{
		Handler:           callbackHandler(state, codes),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	fmt.Fprintf(f.Out, "Opening your browser to sign in. If it does not open, visit:\n\n  %s\n\n", authorize)
	if f.OpenBrowser != nil {
		_ = f.OpenBrowser(authorize)
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var got result
	select {
	case got = <-codes:
	case <-ctx.Done():
		return credentials.Session{}, errors.New("timed out waiting for the browser to finish signing in")
	}
	if got.err != nil {
		return credentials.Session{}, got.err
	}
	return f.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"code":          {got.code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
}

type result struct {
	code string
	err  error
}

// callbackHandler takes the first redirect that carries the expected state.
func callbackHandler(state string, out chan<- result) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "This sign-in does not match the one ivaldictl started.", http.StatusBadRequest)
			return
		}
		var res result
		if e := q.Get("error"); e != "" {
			res.err = fmt.Errorf("sign-in failed: %s", strings.TrimSpace(e+" "+q.Get("error_description")))
		} else if res.code = q.Get("code"); res.code == "" {
			res.err = errors.New("sign-in failed: the redirect carried no code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, page("Sign-in failed", "Return to the terminal for details."))
		} else {
			fmt.Fprint(w, page("Signed in", "You can close this tab and return to the terminal."))
		}
		select {
		case out <- res:
		default:
		}
	})
	return mux
}

func page(title, body string) string {
	return `<!doctype html><html lang="en"><meta charset="utf-8"><title>` + title + ` · ivaldictl</title>` +
		`<body style="font-family:system-ui;margin:4rem"><h1>` + title + `</h1><p>` + body + `</p></body></html>`
}

// Refresh exchanges a refresh token for a new session. The control plane
// rotates refresh tokens, so the old one stops working.
func Refresh(ctx context.Context, httpc *http.Client, server, refreshToken string, now func() time.Time) (credentials.Session, error) {
	return Flow{Server: server, HTTP: httpc, Now: now}.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {refreshToken},
	})
}

// Revoke revokes a refresh token, ending the session at the control plane.
func Revoke(ctx context.Context, httpc *http.Client, server, refreshToken string) error {
	resp, err := post(ctx, httpc, strings.TrimRight(server, "/")+"/oauth/revoke", url.Values{"token": {refreshToken}, "client_id": {ClientID}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke the session: the control plane answered %s", resp.Status)
	}
	return nil
}

// ErrSignInAgain means the session can no longer be refreshed.
var ErrSignInAgain = errors.New("the session ended; run ivaldictl auth login")

func (f Flow) token(ctx context.Context, form url.Values) (credentials.Session, error) {
	resp, err := post(ctx, f.HTTP, strings.TrimRight(f.Server, "/")+"/oauth/token", form)
	if err != nil {
		return credentials.Session{}, err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return credentials.Session{}, fmt.Errorf("read the token response (%s): %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK {
		if body.Error == "invalid_grant" {
			return credentials.Session{}, ErrSignInAgain
		}
		return credentials.Session{}, fmt.Errorf("get tokens: %s %s", body.Error, body.ErrorDescription)
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	return credentials.Session{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func post(ctx context.Context, httpc *http.Client, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the control plane: %w", err)
	}
	return resp, nil
}

// newPKCE returns a code verifier and its S256 challenge.
func newPKCE() (string, string, error) {
	verifier, err := randomString(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate a random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

package connector

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

const (
	clientID = "ivaldi"
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	callback = "https://ivaldi.example.com/auth/oauth/oidc/callback"
)

func s256(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// fakeIssuer is an OpenID Connect provider that signs in whoever claims says.
type fakeIssuer struct {
	*httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any
	// gotVerifier is the PKCE verifier the token request carried.
	gotVerifier string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.URL, "authorization_endpoint": f.URL + "/authorize", "token_endpoint": f.URL + "/token",
			"jwks_uri": f.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.gotVerifier = r.PostForm.Get("code_verifier")
		claims := map[string]any{"iss": f.URL, "aud": clientID, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
		for k, v := range f.claims {
			claims[k] = v
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, nil)
		require.NoError(t, err)
		payload, _ := json.Marshal(claims)
		jws, err := signer.Sign(payload)
		require.NoError(t, err)
		idToken, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": idToken})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestOIDCSignIn(t *testing.T) {
	idp := newFakeIssuer(t)
	idp.claims = map[string]any{"sub": "user-1", "nonce": "n1", "email": "ada@acme.com", "email_verified": "true", "name": "Ada"}
	c, err := NewOIDC(OIDCConfig{IssuerURL: idp.URL, ClientID: clientID, ClientSecret: "s", Name: "Okta", Scopes: []string{"email"}})
	require.NoError(t, err)
	assert.Equal(t, iam.ProtocolOIDC, c.Protocol())
	assert.Equal(t, "Okta", c.DisplayName())

	start, err := c.AuthCodeURL(context.Background(), AuthRequest{State: "st", Nonce: "n1", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	u, _ := url.Parse(start)
	q := u.Query()
	assert.Equal(t, idp.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "n1", q.Get("nonce"))
	assert.Equal(t, s256(verifier), q.Get("code_challenge"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "openid email", q.Get("scope"))

	profile, err := c.Exchange(context.Background(), Callback{Code: "code", Verifier: verifier, Nonce: "n1", RedirectURI: callback})
	require.NoError(t, err)
	assert.Equal(t, verifier, idp.gotVerifier, "the code is redeemed with the PKCE verifier")
	assert.Equal(t, iam.ExternalProfile{Subject: "user-1", Email: "ada@acme.com", EmailVerified: true, DisplayName: "Ada"}, profile)
}

func TestOIDCRefusesAnotherNonce(t *testing.T) {
	idp := newFakeIssuer(t)
	idp.claims = map[string]any{"sub": "user-1", "nonce": "someone-elses"}
	c, err := NewOIDC(OIDCConfig{IssuerURL: idp.URL, ClientID: clientID})
	require.NoError(t, err)

	_, err = c.Exchange(context.Background(), Callback{Code: "code", Verifier: verifier, Nonce: "n1", RedirectURI: callback})

	assert.ErrorIs(t, err, ErrExchange)
}

func TestOIDCDiscoversLazily(t *testing.T) {
	_, err := NewOIDC(OIDCConfig{IssuerURL: "https://unreachable.invalid", ClientID: clientID})
	assert.NoError(t, err, "a provider that is down does not stop the control plane from starting")
}

func TestGoogleReadsTheHostedDomain(t *testing.T) {
	idp := newFakeIssuer(t)
	idp.claims = map[string]any{"sub": "g-1", "nonce": "n1", "email": "ada@acme.com", "email_verified": true, "hd": "acme.com"}
	c, err := NewGoogle(GoogleConfig{ClientID: clientID, issuer: idp.URL})
	require.NoError(t, err)
	assert.Equal(t, iam.ProtocolGoogle, c.Protocol())

	profile, err := c.Exchange(context.Background(), Callback{Code: "code", Verifier: verifier, Nonce: "n1", RedirectURI: callback})

	require.NoError(t, err)
	assert.Equal(t, "acme.com", profile.HostedDomain)
	assert.True(t, profile.EmailVerified)
}

func newFakeGitHub(t *testing.T, emails, orgs any) (*httptest.Server, *url.Values) {
	t.Helper()
	got := &url.Values{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		*got = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gho_x", "token_type": "bearer"})
	})
	api := func(v any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer gho_x", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	mux.HandleFunc("GET /api/v3/user", api(map[string]any{"id": 583231, "login": "octocat", "name": ""}))
	mux.HandleFunc("GET /api/v3/user/emails", api(emails))
	mux.HandleFunc("GET /api/v3/user/memberships/orgs", api(orgs))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, got
}

func TestGitHubSignIn(t *testing.T) {
	srv, got := newFakeGitHub(t,
		[]map[string]any{
			{"email": "old@acme.com", "primary": false, "verified": true},
			{"email": "octo@acme.com", "primary": true, "verified": true},
		},
		[]map[string]any{{"organization": map[string]any{"login": "acme"}}},
	)
	c, err := NewGitHub(GitHubConfig{BaseURL: srv.URL, ClientID: "Iv1.abc", ClientSecret: "s", ReadOrganizations: true})
	require.NoError(t, err)

	start, err := c.AuthCodeURL(context.Background(), AuthRequest{State: "st", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	q := mustParse(t, start).Query()
	assert.Equal(t, "read:user user:email read:org", q.Get("scope"))
	assert.Equal(t, s256(verifier), q.Get("code_challenge"))

	profile, err := c.Exchange(context.Background(), Callback{Code: "code", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	assert.Equal(t, verifier, got.Get("code_verifier"))
	assert.Equal(t, iam.ExternalProfile{
		Subject: "583231", Email: "octo@acme.com", EmailVerified: true, DisplayName: "octocat", Organizations: []string{"acme"},
	}, profile, "the numeric ID is the subject, and only the primary verified email counts")
}

func TestGitHubWithoutAVerifiedEmail(t *testing.T) {
	srv, _ := newFakeGitHub(t, []map[string]any{{"email": "octo@acme.com", "primary": true, "verified": false}}, nil)
	c, err := NewGitHub(GitHubConfig{BaseURL: srv.URL, ClientID: "Iv1.abc"})
	require.NoError(t, err)

	start, err := c.AuthCodeURL(context.Background(), AuthRequest{State: "st", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	assert.NotContains(t, mustParse(t, start).Query().Get("scope"), "read:org", "organizations are only asked for when needed")

	profile, err := c.Exchange(context.Background(), Callback{Code: "code", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	assert.Empty(t, profile.Email)
	assert.False(t, profile.EmailVerified)
	assert.Nil(t, profile.Organizations)
}

func TestGitHubDotComUsesTheHostedAPI(t *testing.T) {
	c, err := NewGitHub(GitHubConfig{ClientID: "Iv1.abc"})
	require.NoError(t, err)
	assert.Equal(t, "https://api.github.com", c.(*githubConnector).api)

	start, err := c.AuthCodeURL(context.Background(), AuthRequest{State: "st", Verifier: verifier, RedirectURI: callback})
	require.NoError(t, err)
	assert.Equal(t, "github.com", mustParse(t, start).Host)
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

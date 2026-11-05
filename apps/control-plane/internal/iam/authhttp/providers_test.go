package authhttp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector/connectortest"
)

func newProviderRig(t *testing.T, subject string) (*rig, *connectortest.Fake) {
	t.Helper()
	gh := &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub", Profile: iam.ExternalProfile{Subject: subject}}
	r := newRig(t, WithProviders(Provider{Kind: "github", Connector: gh, OrganizationID: "org_acme", IdentityProviderID: "idp_gh"}))
	return r, gh
}

// startAndReturn plays the browser leaving for the provider and coming back.
func startAndReturn(t *testing.T, r *rig, back string) *http.Response {
	t.Helper()
	start := "/auth/oauth/github/start"
	if back != "" {
		start += "?" + url.Values{"return_to": {back}}.Encode()
	}
	rec := r.get(start)
	require.Equal(t, http.StatusFound, rec.Code)
	state := mustURL(t, rec.Header().Get("Location")).Query().Get("state")
	return r.get("/auth/oauth/github/callback?" + url.Values{"code": {"c"}, "state": {state}}.Encode()).Result()
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestLoginPageOffersTheProviders(t *testing.T) {
	r, _ := newProviderRig(t, "1")

	body := r.get(LoginPath + "?return_to=" + url.QueryEscape("/oauth/authorize?client_id=ivaldictl")).Body.String()

	assert.Contains(t, body, "Continue with GitHub")
	assert.Contains(t, body, `href="/auth/oauth/github/start?return_to=%2Foauth%2Fauthorize%3Fclient_id%3Divaldictl"`)
}

func TestProviderSignIn(t *testing.T) {
	r, gh := newProviderRig(t, "1001")
	back := "/oauth/authorize?client_id=ivaldictl"

	resp := startAndReturn(t, r, back)

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, back, resp.Header.Get("Location"))
	require.Len(t, gh.Requests, 1)
	require.Len(t, gh.Callbacks, 1)
	assert.Equal(t, "https://ivaldi.example.com/auth/oauth/github/callback", gh.Requests[0].RedirectURI)
	assert.Equal(t, gh.Requests[0].Verifier, gh.Callbacks[0].Verifier, "the PKCE verifier survives the round trip in the cookie")
	assert.Equal(t, gh.Requests[0].Nonce, gh.Callbacks[0].Nonce)
	assert.Len(t, gh.Requests[0].Verifier, 43)

	var got signIn
	require.NoError(t, r.codec.Decode(signInCookie, r.jar["__Host-"+signInCookie].Value, &got))
	assert.Equal(t, signIn{AccountID: "acc_1001", Method: iam.SignInIdentityProvider, IdentityProviderID: "idp_gh", RestrictedTo: "org_acme"}, got)
	assert.NotContains(t, r.jar, "__Host-"+oauthCookie, "the flow's cookie is used once")
}

func TestProviderCallbackRefuses(t *testing.T) {
	t.Run("a state from another browser", func(t *testing.T) {
		r, gh := newProviderRig(t, "1")
		r.get("/auth/oauth/github/start")
		rec := r.get("/auth/oauth/github/callback?code=c&state=forged")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, gh.Callbacks, "the code is never redeemed")
	})
	t.Run("no sign-in was started", func(t *testing.T) {
		r, _ := newProviderRig(t, "1")
		assert.Equal(t, http.StatusBadRequest, r.get("/auth/oauth/github/callback?code=c&state=s").Code)
	})
	t.Run("the person cancelled at the provider", func(t *testing.T) {
		r, _ := newProviderRig(t, "1")
		state := mustURL(t, r.get("/auth/oauth/github/start").Header().Get("Location")).Query().Get("state")
		rec := r.get("/auth/oauth/github/callback?error=access_denied&state=" + state)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "cancelled")
	})
	t.Run("the provider cannot confirm who signed in", func(t *testing.T) {
		r, gh := newProviderRig(t, "1")
		gh.Err = connector.ErrExchange
		assert.Equal(t, http.StatusBadGateway, startAndReturn(t, r, "").StatusCode)
	})
	t.Run("an unknown provider", func(t *testing.T) {
		r, _ := newProviderRig(t, "1")
		assert.Equal(t, http.StatusNotFound, r.get("/auth/oauth/gitlab/start").Code)
	})

	refusals := map[error]int{
		iam.ErrNotAdmitted:   http.StatusForbidden,
		iam.ErrNoMembership:  http.StatusForbidden,
		iam.ErrAccountExists: http.StatusConflict,
		iam.ErrStatus:        http.StatusForbidden,
	}
	for err, status := range refusals {
		t.Run(err.Error(), func(t *testing.T) {
			r, _ := newProviderRig(t, "refused")
			r.auth.external["refused"] = err
			assert.Equal(t, status, startAndReturn(t, r, "").StatusCode)
			assert.NotContains(t, r.jar, "__Host-"+signInCookie)
		})
	}
}

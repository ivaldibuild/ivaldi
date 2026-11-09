package authhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector/connectortest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const (
	testIssuer   = "https://ivaldi.example.com"
	testLoginURL = "https://app.example.com/login"
	clientRedir  = "http://127.0.0.1:5000/cb"
)

var (
	acme  = iam.Membership{OrganizationID: "org_acme", OrganizationSlug: "acme", OrganizationName: "Acme", PrincipalID: "prn_a", OrganizationStatus: iam.OrganizationActive, Status: iam.PrincipalActive}
	other = iam.Membership{OrganizationID: "org_other", OrganizationSlug: "other", OrganizationName: "Other", PrincipalID: "prn_o", OrganizationStatus: iam.OrganizationActive, Status: iam.PrincipalActive}
)

type fakeAuth struct {
	memberships map[string][]iam.Membership
	// external signs in subjects through identity providers, or refuses.
	external map[string]error
}

func (f *fakeAuth) AuthenticatePassword(_ context.Context, email, password string) (*iam.SignedIn, error) {
	switch {
	case email == "ada@acme.com" && password == "pw":
		return &iam.SignedIn{AccountID: "acc_ada", Method: iam.SignInPassword}, nil
	case email == "suspended@acme.com":
		return nil, iam.ErrStatus
	}
	return nil, iam.ErrInvalidCredentials
}

func (f *fakeAuth) AuthenticateExternal(_ context.Context, organizationID, identityProviderID string, p iam.ExternalProfile) (*iam.SignedIn, error) {
	if err := f.external[p.Subject]; err != nil {
		return nil, err
	}
	return &iam.SignedIn{AccountID: "acc_" + p.Subject, Method: iam.SignInIdentityProvider, IdentityProviderID: identityProviderID, RestrictedTo: organizationID}, nil
}

func (f *fakeAuth) AuthenticatePlatform(_ context.Context, _ iam.PlatformProvider, p iam.ExternalProfile) (*iam.SignedIn, error) {
	if err := f.external[p.Subject]; err != nil {
		return nil, err
	}
	return &iam.SignedIn{AccountID: "acc_" + p.Subject, Method: iam.SignInPlatform}, nil
}

func (f *fakeAuth) Memberships(_ context.Context, accountID string) ([]iam.Membership, error) {
	return f.memberships[accountID], nil
}

func (f *fakeAuth) Membership(_ context.Context, accountID, organizationID string) (*iam.Membership, error) {
	for _, m := range f.memberships[accountID] {
		if m.OrganizationID == organizationID {
			return &m, nil
		}
	}
	return nil, iam.ErrNoMembership
}

type fakeServer struct {
	issued    []iam.Membership
	exchanged []iam.TokenRequest
	exchange  func(iam.TokenRequest) (*iam.TokenResponse, error)
}

func (f *fakeServer) ValidateAuthorization(_ context.Context, r iam.AuthorizeRequest) (*iam.ValidatedAuthorization, error) {
	switch {
	case r.ClientID != "ivaldictl":
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "unknown client"}
	case r.CodeChallenge == "":
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "PKCE required", RedirectURI: r.RedirectURI, State: r.State}
	}
	return &iam.ValidatedAuthorization{Client: "ocl_1", ClientID: r.ClientID, ClientName: "Ivaldi CLI", RedirectURI: r.RedirectURI, State: r.State}, nil
}

func (f *fakeServer) IssueCode(_ context.Context, v *iam.ValidatedAuthorization, m iam.Membership) (string, error) {
	f.issued = append(f.issued, m)
	return v.RedirectURI + "?code=c-" + m.OrganizationID + "&state=" + v.State, nil
}

func (f *fakeServer) Exchange(_ context.Context, r iam.TokenRequest) (*iam.TokenResponse, error) {
	f.exchanged = append(f.exchanged, r)
	return f.exchange(r)
}

func (f *fakeServer) Revoke(context.Context, string, string) error { return nil }

func (f *fakeServer) Metadata() iam.AuthorizationServerMetadata {
	return iam.AuthorizationServerMetadata{Issuer: testIssuer}
}

type fakeOnboarding struct {
	got []iam.OnboardWithPlatformIdentityInput
	err error
}

func (f *fakeOnboarding) OnboardOrganization(context.Context, *iam.OnboardOrganizationInput) error {
	return errors.New("unused")
}

func (f *fakeOnboarding) ProvisionInstallation(context.Context, iam.ProvisionInstallationInput) (*iam.Organization, error) {
	return nil, errors.New("unused")
}

func (f *fakeOnboarding) OnboardWithPlatformIdentity(_ context.Context, in iam.OnboardWithPlatformIdentityInput) (string, error) {
	f.got = append(f.got, in)
	if f.err != nil {
		return "", f.err
	}
	return "acc_new", nil
}

// browser plays one browser: it keeps the cookies the server sets.
type browser struct {
	t       *testing.T
	handler http.Handler
	jar     map[string]*http.Cookie
}

type rig struct {
	*browser
	auth   *fakeAuth
	server *fakeServer
	codec  session.Codec
	build  func() http.Handler
}

func newRig(t *testing.T, extra ...Option) *rig {
	t.Helper()
	codec, err := session.NewCodec([][32]byte{{1}})
	require.NoError(t, err)
	logger := logging.NewMockLogger()
	logger.On("Error", mock.Anything, mock.Anything).Maybe()
	logger.On("Warn", mock.Anything, mock.Anything, mock.Anything).Maybe()
	r := &rig{
		auth:   &fakeAuth{memberships: map[string][]iam.Membership{"acc_ada": {acme}}, external: map[string]error{}},
		server: &fakeServer{},
		codec:  codec,
	}
	h, err := New(append([]Option{
		WithSettings(Settings{Issuer: testIssuer, LoginURL: testLoginURL, SessionTTL: 10 * time.Minute, BuiltinAccounts: true}),
		WithLogger(logger), WithAuthentication(r.auth), WithAuthorizationServer(r.server), WithCodec(codec),
	}, extra...)...)
	require.NoError(t, err)
	port := 0
	s, err := api.New(&config.Config{Port: &port, ExternalURL: testIssuer}, "test", logger, api.WithRoutes(h), api.WithHandlers(h))
	require.NoError(t, err)
	r.browser = &browser{t: t, handler: s.Handler(), jar: map[string]*http.Cookie{}}
	return r
}

// another returns a second browser on the same server, with no cookies.
func (b *browser) another() *browser {
	return &browser{t: b.t, handler: b.handler, jar: map[string]*http.Cookie{}}
}

func (b *browser) do(req *http.Request) *httptest.ResponseRecorder {
	for _, c := range b.jar {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.jar, c.Name)
		} else {
			b.jar[c.Name] = c
		}
	}
	return rec
}

func (b *browser) get(target string) *httptest.ResponseRecorder {
	return b.do(httptest.NewRequest(http.MethodGet, strings.TrimPrefix(target, testIssuer), nil))
}

// call is the frontend calling a JSON operation; it carries no cookies.
func (b *browser) call(method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	b.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func location(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	u, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	return u
}

const authorizePath = "/oauth/authorize?response_type=code&client_id=ivaldictl&redirect_uri=http%3A%2F%2F127.0.0.1%3A5000%2Fcb&state=xyz&code_challenge=c&code_challenge_method=S256"

// challenge starts an authorization and returns the login challenge the
// frontend gets.
func (b *browser) challenge(t *testing.T, path string) string {
	t.Helper()
	rec := b.get(path)
	require.Equal(t, http.StatusFound, rec.Code)
	loc := location(t, rec)
	require.Equal(t, "app.example.com", loc.Host, "sent to the frontend")
	return loc.Query().Get("login_challenge")
}

func TestAuthorizeSendsThePersonToTheFrontend(t *testing.T) {
	r := newRig(t)

	ch := r.challenge(t, authorizePath)

	assert.NotEmpty(t, ch)
	assert.Contains(t, r.jar, "__Host-"+loginCookie, "the challenge is bound to this browser")
	rec := r.get(authorizePath)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "text/html", "the control plane serves no pages")
}

func TestAuthorizeErrors(t *testing.T) {
	t.Run("an untrusted client goes to the frontend, not the client", func(t *testing.T) {
		loc := location(t, newRig(t).get("/oauth/authorize?client_id=evil&redirect_uri=https%3A%2F%2Fevil.example.com"))
		assert.Equal(t, "app.example.com", loc.Host)
		assert.Equal(t, "invalid_request", loc.Query().Get("error"))
	})
	t.Run("a trusted client's bad request goes back to it", func(t *testing.T) {
		loc := location(t, newRig(t).get(strings.Replace(authorizePath, "code_challenge=c&", "", 1)))
		assert.Equal(t, "127.0.0.1:5000", loc.Host)
		assert.Equal(t, "invalid_request", loc.Query().Get("error"))
		assert.Equal(t, "xyz", loc.Query().Get("state"))
	})
	t.Run("prompt=none without a sign-in", func(t *testing.T) {
		loc := location(t, newRig(t).get(authorizePath+"&prompt=none"))
		assert.Equal(t, "login_required", loc.Query().Get("error"))
	})
}

func TestLoginRequestDescribesTheSignIn(t *testing.T) {
	gh := &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub"}
	r := newRig(t, WithProviders(Provider{Kind: "github", Connector: gh, OrganizationID: "org_acme", IdentityProviderID: "idp_gh"}))
	ch := r.challenge(t, authorizePath)

	rec, body := r.call(http.MethodGet, "/v1/auth/login?login_challenge="+url.QueryEscape(ch), nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Ivaldi CLI", body["client_name"])
	assert.Equal(t, true, body["password"])
	assert.Equal(t, false, body["sign_up"])
	providers := body["providers"].([]any)
	require.Len(t, providers, 1)
	start := providers[0].(map[string]any)["start_url"].(string)
	assert.True(t, strings.HasPrefix(start, testIssuer+"/auth/oauth/github/start?login_challenge="))

	rec, _ = r.call(http.MethodGet, "/v1/auth/login?login_challenge=forged", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPasswordSignIn(t *testing.T) {
	r := newRig(t)
	ch := r.challenge(t, authorizePath)

	rec, body := r.call(http.MethodPost, "/v1/auth/login/password", map[string]any{"login_challenge": ch, "email": "ada@acme.com", "password": "pw"})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	redirect := body["redirect_to"].(string)
	assert.True(t, strings.HasPrefix(redirect, testIssuer+ContinuePath+"?login_verifier="))

	t.Run("only the browser that started can continue", func(t *testing.T) {
		loc := location(t, r.another().get(redirect))
		assert.Equal(t, "app.example.com", loc.Host)
		assert.Equal(t, errorExpired, loc.Query().Get("error"))
		assert.Empty(t, r.server.issued)
	})

	rec = r.get(redirect)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, clientRedir+"?code=c-org_acme&state=xyz", rec.Header().Get("Location"))
	assert.Contains(t, r.jar, "__Host-"+signInCookie, "the sign-in is remembered")
	assert.NotContains(t, r.jar, "__Host-"+loginCookie, "the challenge is used up")

	t.Run("a remembered sign-in skips the frontend", func(t *testing.T) {
		rec := r.get(authorizePath)
		assert.Equal(t, clientRedir+"?code=c-org_acme&state=xyz", rec.Header().Get("Location"))
	})
	t.Run("prompt=login asks again", func(t *testing.T) {
		assert.Equal(t, "app.example.com", location(t, r.get(authorizePath+"&prompt=login")).Host)
	})
	t.Run("logout forgets it", func(t *testing.T) {
		assert.Equal(t, "app.example.com", location(t, r.get(LogoutPath)).Host)
		assert.NotContains(t, r.jar, "__Host-"+signInCookie)
	})
}

func TestPasswordSignInRefuses(t *testing.T) {
	r := newRig(t)
	ch := r.challenge(t, authorizePath)
	tests := map[string]struct {
		email, password string
		status          int
	}{
		"wrong password": {"ada@acme.com", "nope", http.StatusUnauthorized},
		"suspended":      {"suspended@acme.com", "pw", http.StatusForbidden},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec, _ := r.call(http.MethodPost, "/v1/auth/login/password", map[string]any{"login_challenge": ch, "email": tt.email, "password": tt.password})
			assert.Equal(t, tt.status, rec.Code)
		})
	}
	t.Run("no organization", func(t *testing.T) {
		r.auth.memberships["acc_ada"] = nil
		rec, _ := r.call(http.MethodPost, "/v1/auth/login/password", map[string]any{"login_challenge": ch, "email": "ada@acme.com", "password": "pw"})
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestChoosingAnOrganization(t *testing.T) {
	r := newRig(t)
	r.auth.memberships["acc_ada"] = []iam.Membership{acme, other}
	ch := r.challenge(t, authorizePath)

	rec, body := r.call(http.MethodPost, "/v1/auth/login/password", map[string]any{"login_challenge": ch, "email": "ada@acme.com", "password": "pw"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, body["redirect_to"])
	assert.Len(t, body["organizations"], 2)
	sessionToken := body["login_session"].(string)

	t.Run("not a member", func(t *testing.T) {
		rec, _ := r.call(http.MethodPost, "/v1/auth/login/organization", map[string]any{"login_challenge": ch, "login_session": sessionToken, "organization_id": "org_foreign"})
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
	t.Run("a session from another sign-in", func(t *testing.T) {
		otherChallenge := r.another().challenge(t, authorizePath)
		rec, _ := r.call(http.MethodPost, "/v1/auth/login/organization", map[string]any{"login_challenge": otherChallenge, "login_session": sessionToken, "organization_id": "org_other"})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	rec, body = r.call(http.MethodPost, "/v1/auth/login/organization", map[string]any{"login_challenge": ch, "login_session": sessionToken, "organization_id": "org_other"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, clientRedir+"?code=c-org_other&state=xyz", r.get(body["redirect_to"].(string)).Header().Get("Location"))

	t.Run("a remembered sign-in with several organizations asks the frontend to choose", func(t *testing.T) {
		loc := location(t, r.get(authorizePath))
		assert.Equal(t, "app.example.com", loc.Host)
		assert.NotEmpty(t, loc.Query().Get("login_session"))
		assert.Equal(t, clientRedir+"?code=c-org_acme&state=xyz", r.get(authorizePath+"&organization=acme").Header().Get("Location"), "a hint chooses")
	})
}

func TestListingALoginSessionOrganizations(t *testing.T) {
	r, _ := newProviderRig(t, "1002", true, nil)
	r.auth.memberships["acc_1002"] = []iam.Membership{acme, other}
	ch := r.challenge(t, authorizePath)

	// A provider sign-in with several organizations sends the browser back
	// with only the challenge and the login session.
	loc := location(t, r.viaProvider(t, ch))
	require.Equal(t, "app.example.com", loc.Host)
	sessionToken := loc.Query().Get("login_session")
	require.NotEmpty(t, sessionToken)
	list := func(challenge, session string) (*httptest.ResponseRecorder, map[string]any) {
		return r.call(http.MethodPost, "/v1/auth/login/organizations", map[string]any{"login_challenge": challenge, "login_session": session})
	}

	rec, body := list(ch, sessionToken)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotEmpty(t, body["login_session"], "the answer is a step the frontend can choose from")
	organizations := body["organizations"].([]any)
	require.Len(t, organizations, 2)
	assert.Equal(t, "org_acme", organizations[0].(map[string]any)["id"])
	assert.Equal(t, "acme", organizations[0].(map[string]any)["slug"])
	assert.Equal(t, "Acme", organizations[0].(map[string]any)["name"])

	t.Run("a session from another sign-in", func(t *testing.T) {
		otherChallenge := r.another().challenge(t, authorizePath)
		rec, _ := list(otherChallenge, sessionToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("forged values", func(t *testing.T) {
		rec, _ := list("forged", "forged")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("only one organization left finishes the sign-in", func(t *testing.T) {
		r.auth.memberships["acc_1002"] = []iam.Membership{other}
		rec, body := list(ch, sessionToken)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, clientRedir+"?code=c-org_other&state=xyz", r.get(body["redirect_to"].(string)).Header().Get("Location"))
	})
	t.Run("no organization left is refused", func(t *testing.T) {
		r.auth.memberships["acc_1002"] = nil
		rec, _ := list(ch, sessionToken)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func newProviderRig(t *testing.T, subject string, platform bool, ob *fakeOnboarding) (*rig, *connectortest.Fake) {
	t.Helper()
	gh := &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub", Profile: iam.ExternalProfile{Subject: subject, Email: "grace@example.com", EmailVerified: true}}
	p := Provider{Kind: "github", Connector: gh, OrganizationID: "org_acme", IdentityProviderID: "idp_gh"}
	if platform {
		p = Provider{Kind: "github", Connector: gh, Platform: iam.PlatformGitHub}
	}
	opts := []Option{WithProviders(p)}
	if ob != nil {
		opts = append(opts, WithOnboarding(ob))
	}
	r := newRig(t, opts...)
	r.auth.memberships["acc_"+subject] = []iam.Membership{acme}
	return r, gh
}

// viaProvider starts at the provider with the challenge and comes back.
func (b *browser) viaProvider(t *testing.T, ch string) *httptest.ResponseRecorder {
	t.Helper()
	rec := b.get("/auth/oauth/github/start?login_challenge=" + url.QueryEscape(ch))
	require.Equal(t, http.StatusFound, rec.Code)
	state := location(t, rec).Query().Get("state")
	return b.get("/auth/oauth/github/callback?" + url.Values{"code": {"c"}, "state": {state}}.Encode())
}

func TestProviderSignIn(t *testing.T) {
	r, gh := newProviderRig(t, "1001", false, nil)
	ch := r.challenge(t, authorizePath)

	rec := r.viaProvider(t, ch)

	require.Equal(t, http.StatusFound, rec.Code)
	assert.True(t, strings.HasPrefix(rec.Header().Get("Location"), testIssuer+ContinuePath))
	assert.Equal(t, gh.Requests[0].Verifier, gh.Callbacks[0].Verifier, "the PKCE verifier survives the round trip")
	assert.Equal(t, testIssuer+"/auth/oauth/github/callback", gh.Requests[0].RedirectURI)

	rec = r.get(rec.Header().Get("Location"))
	assert.Equal(t, clientRedir+"?code=c-org_acme&state=xyz", rec.Header().Get("Location"))
}

// A GitHub App that also signs people in may return from an install to the
// sign-in callback; the callback sends it on to the web app's install page.
func TestProviderCallbackSendsAnInstallReturnOn(t *testing.T) {
	const page = "https://app.example.com/settings/connections/github/callback"
	r, gh := newProviderRig(t, "1", false, nil)
	r2 := newRig(t, WithProviders(Provider{Kind: "github", Connector: gh, OrganizationID: "org_acme", IdentityProviderID: "idp_gh"}),
		WithInstallReturns(map[string]string{"github": page}))

	// A sign-in in progress in the same browser keeps its cookie.
	ch := r2.challenge(t, authorizePath)
	r2.get("/auth/oauth/github/start?login_challenge=" + url.QueryEscape(ch))
	hasFlow := func() bool {
		for name := range r2.jar {
			if strings.HasSuffix(name, oauthCookie) {
				return true
			}
		}
		return false
	}
	require.True(t, hasFlow())

	install := url.Values{"code": {"c"}, "installation_id": {"42"}, "setup_action": {"install"}, "state": {"S"}}
	rec := r2.get("/auth/oauth/github/callback?" + install.Encode())

	require.Equal(t, http.StatusFound, rec.Code)
	loc := location(t, rec)
	assert.Equal(t, "app.example.com", loc.Host)
	assert.Equal(t, "/settings/connections/github/callback", loc.Path)
	assert.Equal(t, install, loc.Query(), "the code, installation, and state go on unchanged")
	assert.Empty(t, gh.Callbacks, "the code is not redeemed as a sign-in")
	assert.True(t, hasFlow(), "the sign-in in progress keeps its cookie")

	t.Run("without install returns configured, it is an expired sign-in", func(t *testing.T) {
		loc := location(t, r.get("/auth/oauth/github/callback?"+install.Encode()))
		assert.Equal(t, errorExpired, loc.Query().Get("error"))
	})
}

func TestProviderCallbackErrors(t *testing.T) {
	t.Run("a state from another browser", func(t *testing.T) {
		r, gh := newProviderRig(t, "1", false, nil)
		ch := r.challenge(t, authorizePath)
		r.get("/auth/oauth/github/start?login_challenge=" + url.QueryEscape(ch))
		loc := location(t, r.get("/auth/oauth/github/callback?code=c&state=forged"))
		assert.Equal(t, errorExpired, loc.Query().Get("error"))
		assert.Empty(t, gh.Callbacks, "the code is never redeemed")
	})
	t.Run("start without a challenge", func(t *testing.T) {
		r, _ := newProviderRig(t, "1", false, nil)
		assert.Equal(t, errorExpired, location(t, r.get("/auth/oauth/github/start")).Query().Get("error"))
	})
	t.Run("cancelled at the provider", func(t *testing.T) {
		r, _ := newProviderRig(t, "1", false, nil)
		ch := r.challenge(t, authorizePath)
		state := location(t, r.get("/auth/oauth/github/start?login_challenge="+url.QueryEscape(ch))).Query().Get("state")
		loc := location(t, r.get("/auth/oauth/github/callback?error=access_denied&state="+state))
		assert.Equal(t, errorCancelled, loc.Query().Get("error"))
		assert.Equal(t, ch, loc.Query().Get("login_challenge"), "the person can try another way")
	})
	t.Run("the provider cannot confirm", func(t *testing.T) {
		r, gh := newProviderRig(t, "1", false, nil)
		gh.Err = connector.ErrExchange
		assert.Equal(t, errorProvider, location(t, r.viaProvider(t, r.challenge(t, authorizePath))).Query().Get("error"))
	})
	for err, code := range map[error]string{
		iam.ErrNotAdmitted:   errorNotAdmitted,
		iam.ErrNoMembership:  errorNoMembership,
		iam.ErrAccountExists: errorAccountExists,
		iam.ErrStatus:        errorDisabled,
	} {
		t.Run(code, func(t *testing.T) {
			r, _ := newProviderRig(t, "refused", false, nil)
			r.auth.external["refused"] = err
			loc := location(t, r.viaProvider(t, r.challenge(t, authorizePath)))
			assert.Equal(t, "app.example.com", loc.Host)
			assert.Equal(t, code, loc.Query().Get("error"))
		})
	}
}

func TestPlatformSignUp(t *testing.T) {
	ob := &fakeOnboarding{}
	r, _ := newProviderRig(t, "new", true, ob)
	r.auth.external["new"] = iam.ErrSignUpRequired
	r.auth.memberships["acc_new"] = []iam.Membership{acme}
	ch := r.challenge(t, authorizePath)

	_, info := r.call(http.MethodGet, "/v1/auth/login?login_challenge="+url.QueryEscape(ch), nil)
	assert.Equal(t, true, info["sign_up"])

	loc := location(t, r.viaProvider(t, ch))
	assert.Equal(t, "app.example.com", loc.Host)
	assert.Equal(t, "grace@example.com", loc.Query().Get("email"))
	token := loc.Query().Get("signup_token")
	require.NotEmpty(t, token)

	rec, body := r.call(http.MethodPost, "/v1/auth/signup", map[string]any{"login_challenge": ch, "signup_token": token, "organization_name": "Compilers Inc", "organization_slug": "compilers"})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, ob.got, 1)
	assert.Equal(t, iam.OnboardWithPlatformIdentityInput{
		Provider:         iam.PlatformGitHub,
		Profile:          iam.ExternalProfile{Subject: "new", Email: "grace@example.com", EmailVerified: true},
		OrganizationName: "Compilers Inc", OrganizationSlug: new("compilers"),
	}, ob.got[0])
	assert.Equal(t, clientRedir+"?code=c-org_acme&state=xyz", r.get(body["redirect_to"].(string)).Header().Get("Location"))
}

func TestPlatformSignUpRefuses(t *testing.T) {
	t.Run("a taken name", func(t *testing.T) {
		r, _ := newProviderRig(t, "new", true, &fakeOnboarding{err: onboarding.NewErrOrganizationConflict("taken")})
		r.auth.external["new"] = iam.ErrSignUpRequired
		ch := r.challenge(t, authorizePath)
		token := location(t, r.viaProvider(t, ch)).Query().Get("signup_token")
		rec, _ := r.call(http.MethodPost, "/v1/auth/signup", map[string]any{"login_challenge": ch, "signup_token": token, "organization_name": "Taken"})
		assert.Equal(t, http.StatusConflict, rec.Code)
	})
	t.Run("a forged token", func(t *testing.T) {
		r, _ := newProviderRig(t, "new", true, &fakeOnboarding{})
		ch := r.challenge(t, authorizePath)
		rec, _ := r.call(http.MethodPost, "/v1/auth/signup", map[string]any{"login_challenge": ch, "signup_token": "forged", "organization_name": "X"})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("sign-up turned off", func(t *testing.T) {
		r, _ := newProviderRig(t, "new", true, nil)
		r.auth.external["new"] = iam.ErrSignUpRequired
		assert.Equal(t, errorNoMembership, location(t, r.viaProvider(t, r.challenge(t, authorizePath))).Query().Get("error"))
	})
}

func TestTokenEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		result func(iam.TokenRequest) (*iam.TokenResponse, error)
		status int
	}{
		{"success", func(iam.TokenRequest) (*iam.TokenResponse, error) {
			return &iam.TokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 900, RefreshToken: "ivr_x"}, nil
		}, http.StatusOK},
		{"invalid grant", func(iam.TokenRequest) (*iam.TokenResponse, error) {
			return nil, &iam.OAuthError{Code: iam.OAuthInvalidGrant}
		}, http.StatusBadRequest},
		{"invalid client", func(iam.TokenRequest) (*iam.TokenResponse, error) {
			return nil, &iam.OAuthError{Code: iam.OAuthInvalidClient}
		}, http.StatusUnauthorized},
		{"failure", func(iam.TokenRequest) (*iam.TokenResponse, error) { return nil, errors.New("db down") }, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.server.exchange = tt.result
			req := httptest.NewRequest(http.MethodPost, iam.OAuthTokenPath, strings.NewReader("grant_type=authorization_code&client_id=ivaldictl&code=c"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := r.do(req)
			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.NotContains(t, rec.Body.String(), "db down", "internal errors stay internal")
		})
	}
}

func TestMetadataEndpoint(t *testing.T) {
	rec := newRig(t).get(iam.AuthorizationServerMetadataPath)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"issuer":"https://ivaldi.example.com"`)
}

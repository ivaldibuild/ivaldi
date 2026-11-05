package authhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const testIssuer = "https://ivaldi.example.com"

var (
	acme  = iam.Membership{OrganizationID: "org_acme", OrganizationSlug: "acme", OrganizationName: "Acme", PrincipalID: "prn_a", OrganizationStatus: iam.OrganizationActive, Status: iam.PrincipalActive}
	other = iam.Membership{OrganizationID: "org_other", OrganizationSlug: "other", OrganizationName: "Other", PrincipalID: "prn_o", OrganizationStatus: iam.OrganizationActive, Status: iam.PrincipalActive}
)

type fakeAuth struct {
	memberships map[string][]iam.Membership
	// external signs in subjects through identity providers.
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
	return &iam.ValidatedAuthorization{ClientID: r.ClientID, RedirectURI: r.RedirectURI, State: r.State}, nil
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

type rig struct {
	mux    *http.ServeMux
	auth   *fakeAuth
	server *fakeServer
	codec  session.Codec
	jar    map[string]*http.Cookie
}

func newRig(t *testing.T, extra ...Option) *rig {
	t.Helper()
	codec, err := session.NewCodec([][32]byte{{1}})
	require.NoError(t, err)
	logger := logging.NewMockLogger()
	logger.On("Error", mock.Anything, mock.Anything).Maybe()
	r := &rig{
		mux:    http.NewServeMux(),
		auth:   &fakeAuth{memberships: map[string][]iam.Membership{"acc_ada": {acme}}, external: map[string]error{}},
		server: &fakeServer{},
		codec:  codec,
		jar:    map[string]*http.Cookie{},
	}
	logger.On("Warn", mock.Anything, mock.Anything, mock.Anything).Maybe()
	h, err := New(append([]Option{
		WithSettings(Settings{Issuer: testIssuer, SessionTTL: 10 * time.Minute, BuiltinAccounts: true}),
		WithLogger(logger), WithAuthentication(r.auth), WithAuthorizationServer(r.server), WithCodec(codec),
	}, extra...)...)
	require.NoError(t, err)
	h.RegisterHandlers(r.mux)
	return r
}

// do sends a request with the rig's cookies and keeps the ones it sets.
func (r *rig) do(req *http.Request) *httptest.ResponseRecorder {
	for _, c := range r.jar {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(r.jar, c.Name)
		} else {
			r.jar[c.Name] = c
		}
	}
	return rec
}

func (r *rig) get(path string) *httptest.ResponseRecorder {
	return r.do(httptest.NewRequest(http.MethodGet, path, nil))
}

func (r *rig) post(path string, form url.Values, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return r.do(req)
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (r *rig) csrf(t *testing.T) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(r.get(LoginPath).Body.String())
	require.NotNil(t, m)
	return m[1]
}

func (r *rig) signInAs(t *testing.T, accountID, restrictedTo string) {
	t.Helper()
	v, err := r.codec.Encode(signInCookie, signIn{AccountID: accountID, RestrictedTo: restrictedTo}, time.Minute)
	require.NoError(t, err)
	r.jar["__Host-"+signInCookie] = &http.Cookie{Name: "__Host-" + signInCookie, Value: v}
}

const authorizePath = "/oauth/authorize?response_type=code&client_id=ivaldictl&redirect_uri=http%3A%2F%2F127.0.0.1%3A5000%2Fcb&state=xyz&code_challenge=c&code_challenge_method=S256"

func TestAuthorize(t *testing.T) {
	t.Run("an untrusted client gets an error page, not a redirect", func(t *testing.T) {
		rec := newRig(t).get("/oauth/authorize?client_id=evil&redirect_uri=https%3A%2F%2Fevil.example.com")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, rec.Header().Get("Location"))
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	})
	t.Run("a trusted client's bad request goes back to it", func(t *testing.T) {
		rec := newRig(t).get(strings.Replace(authorizePath, "code_challenge=c&", "", 1))
		require.Equal(t, http.StatusFound, rec.Code)
		loc, _ := url.Parse(rec.Header().Get("Location"))
		assert.Equal(t, "127.0.0.1:5000", loc.Host)
		assert.Equal(t, "invalid_request", loc.Query().Get("error"))
		assert.Equal(t, "xyz", loc.Query().Get("state"))
	})
	t.Run("signing in first, without prompt=login on the way back", func(t *testing.T) {
		rec := newRig(t).get(authorizePath + "&prompt=login")
		require.Equal(t, http.StatusFound, rec.Code)
		loc, _ := url.Parse(rec.Header().Get("Location"))
		assert.Equal(t, LoginPath, loc.Path)
		back := loc.Query().Get("return_to")
		assert.True(t, strings.HasPrefix(back, "/oauth/authorize?"))
		assert.NotContains(t, back, "prompt")
	})
	t.Run("prompt=none without a sign-in", func(t *testing.T) {
		rec := newRig(t).get(authorizePath + "&prompt=none")
		loc, _ := url.Parse(rec.Header().Get("Location"))
		assert.Equal(t, "login_required", loc.Query().Get("error"))
	})
	t.Run("the only membership is chosen", func(t *testing.T) {
		r := newRig(t)
		r.signInAs(t, "acc_ada", "")
		rec := r.get(authorizePath)
		require.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "http://127.0.0.1:5000/cb?code=c-org_acme&state=xyz", rec.Header().Get("Location"))
	})
	t.Run("several memberships need a choice", func(t *testing.T) {
		r := newRig(t)
		r.auth.memberships["acc_ada"] = []iam.Membership{acme, other}
		r.signInAs(t, "acc_ada", "")
		rec := r.get(authorizePath)
		loc, _ := url.Parse(rec.Header().Get("Location"))
		assert.Equal(t, OrganizationsPath, loc.Path)

		rec = r.get(authorizePath + "&organization=other")
		assert.Equal(t, "http://127.0.0.1:5000/cb?code=c-org_other&state=xyz", rec.Header().Get("Location"), "a hint names one by slug or ID")
	})
	t.Run("a restricted sign-in only sees its Organization", func(t *testing.T) {
		r := newRig(t)
		r.auth.memberships["acc_ada"] = []iam.Membership{acme, other}
		r.signInAs(t, "acc_ada", "org_other")
		rec := r.get(authorizePath + "&organization=acme")
		loc, _ := url.Parse(rec.Header().Get("Location"))
		assert.Equal(t, OrganizationsPath, loc.Path, "the hint names an Organization the sign-in cannot use")
		assert.Empty(t, r.server.issued)
	})
	t.Run("no usable membership", func(t *testing.T) {
		r := newRig(t)
		r.signInAs(t, "acc_nobody", "")
		loc, _ := url.Parse(r.get(authorizePath).Header().Get("Location"))
		assert.Equal(t, "access_denied", loc.Query().Get("error"))
	})
	t.Run("a tampered sign-in cookie is no sign-in", func(t *testing.T) {
		r := newRig(t)
		r.jar["__Host-"+signInCookie] = &http.Cookie{Name: "__Host-" + signInCookie, Value: "forged"}
		loc, _ := url.Parse(r.get(authorizePath).Header().Get("Location"))
		assert.Equal(t, LoginPath, loc.Path)
	})
}

func TestLogin(t *testing.T) {
	back := "/oauth/authorize?client_id=ivaldictl"
	form := func(csrf, email, password, returnTo string) url.Values {
		return url.Values{"csrf": {csrf}, "email": {email}, "password": {password}, "return_to": {returnTo}}
	}

	t.Run("signs in and continues the authorization", func(t *testing.T) {
		r := newRig(t)
		rec := r.post(LoginPath, form(r.csrf(t), "ada@acme.com", "pw", back), testIssuer)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, back, rec.Header().Get("Location"))

		var got signIn
		c := r.jar["__Host-"+signInCookie]
		require.NotNil(t, c, "the sign-in cookie is set")
		assert.True(t, c.Secure)
		assert.True(t, c.HttpOnly)
		assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
		require.NoError(t, r.codec.Decode(signInCookie, c.Value, &got))
		assert.Equal(t, "acc_ada", got.AccountID)
	})
	t.Run("wrong password shows the form again", func(t *testing.T) {
		r := newRig(t)
		rec := r.post(LoginPath, form(r.csrf(t), "ada@acme.com", "nope", back), testIssuer)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Body.String(), "The email or password is wrong.")
		assert.NotContains(t, r.jar, "__Host-"+signInCookie)
	})
	t.Run("suspended", func(t *testing.T) {
		r := newRig(t)
		assert.Equal(t, http.StatusForbidden, r.post(LoginPath, form(r.csrf(t), "suspended@acme.com", "pw", back), testIssuer).Code)
	})
	t.Run("without the CSRF token", func(t *testing.T) {
		r := newRig(t)
		r.csrf(t)
		assert.Equal(t, http.StatusForbidden, r.post(LoginPath, form("guess", "ada@acme.com", "pw", back), testIssuer).Code)
	})
	t.Run("from another origin", func(t *testing.T) {
		r := newRig(t)
		assert.Equal(t, http.StatusForbidden, r.post(LoginPath, form(r.csrf(t), "ada@acme.com", "pw", back), "https://evil.example.com").Code)
	})
	t.Run("never redirects off the authorization endpoint", func(t *testing.T) {
		for _, evil := range []string{"https://evil.example.com/oauth/authorize", "//evil.example.com/oauth/authorize", "/admin", `/\evil.example.com`} {
			r := newRig(t)
			rec := r.post(LoginPath, form(r.csrf(t), "ada@acme.com", "pw", evil), testIssuer)
			assert.Equal(t, http.StatusOK, rec.Code, evil)
			assert.Empty(t, rec.Header().Get("Location"), evil)
		}
	})
}

func TestChooseOrganization(t *testing.T) {
	r := newRig(t)
	r.auth.memberships["acc_ada"] = []iam.Membership{acme, other}
	r.signInAs(t, "acc_ada", "")
	back := "/oauth/authorize?client_id=ivaldictl"

	page := r.get(OrganizationsPath + "?return_to=" + url.QueryEscape(back))
	assert.Contains(t, page.Body.String(), "Acme")
	assert.Contains(t, page.Body.String(), "Other")

	rec := r.post(OrganizationsPath, url.Values{"csrf": {r.csrf(t)}, "organization": {"org_other"}, "return_to": {back}}, testIssuer)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, back+"&organization=org_other", rec.Header().Get("Location"))

	rec = r.post(OrganizationsPath, url.Values{"csrf": {r.csrf(t)}, "organization": {"org_foreign"}, "return_to": {back}}, testIssuer)
	assert.Equal(t, http.StatusForbidden, rec.Code)
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
			rec := r.post(iam.OAuthTokenPath, url.Values{"grant_type": {"authorization_code"}, "client_id": {"ivaldictl"}, "code": {"c"}}, "")
			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.NotContains(t, rec.Body.String(), "db down", "internal errors stay internal")
			require.Len(t, r.server.exchanged, 1)
			assert.Equal(t, "ivaldictl", r.server.exchanged[0].ClientID)
		})
	}
}

func TestMetadataEndpoint(t *testing.T) {
	rec := newRig(t).get(iam.AuthorizationServerMetadataPath)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"issuer":"https://ivaldi.example.com"`)
}

func TestLogout(t *testing.T) {
	r := newRig(t)
	r.signInAs(t, "acc_ada", "")
	rec := r.post(LogoutPath, url.Values{"csrf": {r.csrf(t)}}, testIssuer)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, r.jar, "__Host-"+signInCookie)
}

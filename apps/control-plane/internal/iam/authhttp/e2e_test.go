package authhttp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher/hashertest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector/connectortest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/oauthserver"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/signin"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const (
	cliRedirect = "http://127.0.0.1:9999/callback"
	loginPage   = "https://app.example.com/login"
	verifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

type nopOutbox struct{}

func (nopOutbox) Record(context.Context, ...events.Envelope) error { return nil }

// whoami is a secured operation, standing in for any capability's.
type whoami struct{}

type whoamiOutput struct {
	Body struct {
		PrincipalID    string `json:"principal_id"`
		OrganizationID string `json:"organization_id"`
	}
}

func (whoami) RegisterRoutes(a huma.API) {
	huma.Register(a, huma.Operation{OperationID: "whoami", Method: http.MethodGet, Path: "/whoami", Security: []map[string][]string{{api.BearerAuth: {}}}},
		func(ctx context.Context, _ *struct{}) (*whoamiOutput, error) {
			c, _ := identity.CallerFrom(ctx)
			out := &whoamiOutput{}
			out.Body.PrincipalID, out.Body.OrganizationID = c.Actor.PrincipalID, c.OrganizationID
			return out, nil
		})
}

// controlPlane is a running control plane over a throwaway database, wired
// as serve wires it, with a fake GitHub.
type controlPlane struct {
	*httptest.Server
	db     *gorm.DB
	org    *iam.Organization
	user   *iam.User
	github *connectortest.Fake
}

func mustDo[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// start runs a self-hosted control plane, or the managed cloud, where
// GitHub is a platform provider people sign up with.
func start(t *testing.T, cloud bool) *controlPlane {
	t.Helper()
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS)
	srv := httptest.NewUnstartedServer(nil)
	issuer := "http://" + srv.Listener.Addr().String()
	cp := &controlPlane{db: db, github: &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub"}}

	hasher := hashertest.NewFake()
	require.NoError(t, iampg.NewOAuthClientRepository(db).Write(ctx, mustDo(iam.NewOAuthClient(iam.NewOAuthClientProps{
		ClientID: "ivaldictl", Registration: iam.RegistrationFirstParty, Name: "Ivaldi CLI", RedirectURIs: []string{"http://127.0.0.1/callback"},
	}, nil))))

	var provider authhttp.Provider
	if cloud {
		provider = authhttp.Provider{Kind: "github", Connector: cp.github, Platform: iam.PlatformGitHub}
	} else {
		cp.org = mustDo(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, nil))
		require.NoError(t, iampg.NewOrganizationRepository(db).Write(ctx, cp.org))
		require.NoError(t, iampg.NewResourceRegistrar(db).RegisterResource(ctx, cp.org.Resource()))
		account := mustDo(iam.NewAccount(iam.NewAccountProps{Email: "ada@acme.com", DisplayName: "Ada"}, nil))
		require.NoError(t, iampg.NewAccountRepository(db).Write(ctx, account))
		hash, _ := hasher.Hash("correct-horse")
		require.NoError(t, iampg.NewPasswordCredentialRepository(db).Write(ctx, mustDo(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: account.ID(), PasswordHash: hash}))))
		cp.user = mustDo(iam.NewUser(iam.NewUserProps{PrincipalProps: iam.PrincipalProps{OrganizationID: cp.org.ID(), DisplayName: "Ada"}, AccountID: account.ID()}, nil))
		require.NoError(t, iampg.NewUserRepository(db).Write(ctx, cp.user))
		github := mustDo(iam.NewIdentityProvider(iam.NewIdentityProviderProps{
			OrganizationID: cp.org.ID(), Protocol: iam.ProtocolGitHub, Name: "GitHub",
			Config:                 iam.IdentityProviderConfig{ClientID: "Iv1.abc", AllowedOrganizations: []string{"acme"}},
			ManagedByConfiguration: true, JITProvisioning: true,
		}, nil))
		require.NoError(t, iampg.NewIdentityProviderRepository(db).Write(ctx, github))
		provider = authhttp.Provider{Kind: "github", Connector: cp.github, OrganizationID: cp.org.ID(), IdentityProviderID: github.ID()}
	}

	logger := logging.NewMockLogger()
	for _, level := range []string{"Info", "Warn", "Error"} {
		logger.On(level, mock.Anything, mock.Anything).Maybe()
		logger.On(level, mock.Anything, mock.Anything, mock.Anything).Maybe()
	}
	tx := database.NewTransactor(db)
	auth := mustDo(signin.New(
		signin.WithLogger(logger),
		signin.WithOrganizationRepo(iampg.NewOrganizationRepository(db)),
		signin.WithAccountRepo(iampg.NewAccountRepository(db)),
		signin.WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(db)),
		signin.WithUserRepo(iampg.NewUserRepository(db)),
		signin.WithIdentityProviderRepo(iampg.NewIdentityProviderRepository(db)),
		signin.WithExternalIdentityRepo(iampg.NewExternalIdentityRepository(db)),
		signin.WithAccountIdentityRepo(iampg.NewAccountIdentityRepository(db)),
		signin.WithGrantRepo(iampg.NewGrantRepository(db)),
		signin.WithPasswordHasher(hasher),
		signin.WithTransactor(tx),
		signin.WithOutboxRecorder(nopOutbox{}),
		signin.WithBuiltinAccounts(!cloud),
		signin.WithBootstrapAdminEmail("admin@acme.com"),
	))
	ob := mustDo(onboarding.NewOnboardingService(
		onboarding.WithLogger(logger),
		onboarding.WithOrganizationRepo(iampg.NewOrganizationRepository(db)),
		onboarding.WithAccountRepo(iampg.NewAccountRepository(db)),
		onboarding.WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(db)),
		onboarding.WithUserRepo(iampg.NewUserRepository(db)),
		onboarding.WithTransactor(tx),
		onboarding.WithOutboxRecorder(nopOutbox{}),
		onboarding.WithPasswordHasher(hasher),
		onboarding.WithResourceRegistrar(iampg.NewResourceRegistrar(db)),
		onboarding.WithGrantRepo(iampg.NewGrantRepository(db)),
		onboarding.WithAccountIdentityRepo(iampg.NewAccountIdentityRepository(db)),
		onboarding.WithOrganizationProvisioner(&provisioned{}),
	))
	keys := mustDo(token.NewEphemeralKeyRing())
	server := mustDo(oauthserver.New(
		oauthserver.WithSettings(oauthserver.Settings{Issuer: issuer, Resources: []string{issuer}, CodeTTL: time.Minute, RefreshTTL: time.Hour, AccessTTL: 15 * time.Minute}),
		oauthserver.WithLogger(logger),
		oauthserver.WithIssuer(mustDo(token.NewIssuer(keys, issuer, 15*time.Minute))),
		oauthserver.WithAuthentication(auth),
		oauthserver.WithOAuthClientRepo(iampg.NewOAuthClientRepository(db)),
		oauthserver.WithAuthorizationCodeRepo(iampg.NewOAuthAuthorizationCodeRepository(db)),
		oauthserver.WithRefreshTokenRepo(iampg.NewOAuthRefreshTokenRepository(db)),
		oauthserver.WithUserRepo(iampg.NewUserRepository(db)),
		oauthserver.WithTransactor(tx),
		oauthserver.WithOutboxRecorder(nopOutbox{}),
	))
	opts := []authhttp.Option{
		authhttp.WithSettings(authhttp.Settings{Issuer: issuer, LoginURL: loginPage, SessionTTL: 10 * time.Minute, BuiltinAccounts: !cloud}),
		authhttp.WithLogger(logger), authhttp.WithAuthentication(auth), authhttp.WithAuthorizationServer(server),
		authhttp.WithCodec(mustDo(session.NewCodec(mustDo(keys.DeriveKeys(session.KeyPurpose))))),
		authhttp.WithProviders(provider),
	}
	if cloud {
		opts = append(opts, authhttp.WithOnboarding(ob))
	}
	authServer := mustDo(authhttp.New(opts...))
	v := mustDo(identity.NewVerifier(identity.VerifierConfig{Issuer: issuer, Audience: issuer, Keys: identity.NewStaticKeySource(keys.PublicKeys()...)}))

	port := 0
	apiServer := mustDo(api.New(&config.Config{Port: &port, ExternalURL: issuer}, "test", logger,
		api.WithRoutes(whoami{}, authServer), api.WithHandlers(authhttp.NewWellKnown(keys), authServer), api.WithVerifier(v)))
	srv.Config.Handler = apiServer.Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	cp.Server = srv
	return cp
}

// person plays a browser plus the frontend it runs: the browser follows
// the control plane's redirects, and the frontend calls its JSON operations.
type person struct {
	t       *testing.T
	cp      *controlPlane
	browser *http.Client
}

func (cp *controlPlane) newSession(t *testing.T) *person {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &person{t: t, cp: cp, browser: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// visit makes the browser open target and returns where it is sent next.
func (s *person) visit(target string) *url.URL {
	s.t.Helper()
	if strings.HasPrefix(target, "/") {
		target = s.cp.URL + target
	}
	resp, err := s.browser.Get(target)
	require.NoError(s.t, err)
	require.Equal(s.t, http.StatusFound, resp.StatusCode, target)
	return mustDo(url.Parse(resp.Header.Get("Location")))
}

// authorize starts ivaldictl's authorization and returns the login
// challenge the frontend gets.
func (s *person) authorize() string {
	s.t.Helper()
	sum := sha256.Sum256([]byte(verifier))
	to := s.visit("/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {"ivaldictl"}, "redirect_uri": {cliRedirect}, "state": {"s1"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode())
	require.Equal(s.t, "app.example.com", to.Host, "the frontend signs the person in")
	return to.Query().Get("login_challenge")
}

// call is the frontend calling a JSON operation.
func (s *person) call(method, path string, body any, status int) map[string]any {
	s.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, s.cp.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(s.t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(s.t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(s.t, status, resp.StatusCode, out)
	return out
}

// viaGitHub makes the browser sign in at the fake GitHub and returns where
// the callback sends it.
func (s *person) viaGitHub(challenge string) *url.URL {
	s.t.Helper()
	atGitHub := s.visit("/auth/oauth/github/start?" + url.Values{"login_challenge": {challenge}}.Encode())
	return s.visit("/auth/oauth/github/callback?" + url.Values{"code": {"gh-code"}, "state": {atGitHub.Query().Get("state")}}.Encode())
}

// code finishes at the client's callback and returns the code.
func (s *person) code(continueURL string) string {
	s.t.Helper()
	back := s.visit(continueURL)
	require.Equal(s.t, "127.0.0.1:9999", back.Host)
	assert.Equal(s.t, "s1", back.Query().Get("state"))
	return back.Query().Get("code")
}

func exchange(t *testing.T, base string, form url.Values, status int) map[string]any {
	t.Helper()
	resp, err := http.PostForm(base+"/oauth/token", form)
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, status, resp.StatusCode, body)
	return body
}

func redeem(t *testing.T, base, code string) map[string]any {
	t.Helper()
	return exchange(t, base, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"ivaldictl"}, "code": {code},
		"redirect_uri": {cliRedirect}, "code_verifier": {verifier},
	}, http.StatusOK)
}

func whoAmI(t *testing.T, base, accessToken string) whoamiOutput {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out whoamiOutput
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out.Body))
	return out
}

func (cp *controlPlane) owners(t *testing.T, principalID string) int64 {
	t.Helper()
	var n int64
	role, _ := iam.BuiltInRoleID(iam.RoleOwner)
	require.NoError(t, cp.db.Raw("SELECT count(*) FROM iam.grants WHERE role_id = ? AND principal_id = ?", role, principalID).Scan(&n).Error)
	return n
}

func TestSignInWithAPasswordThroughToAnAPICall(t *testing.T) {
	cp := start(t, false)
	s := cp.newSession(t)
	challenge := s.authorize()

	info := s.call(http.MethodGet, "/v1/auth/login?login_challenge="+url.QueryEscape(challenge), nil, http.StatusOK)
	assert.Equal(t, "Ivaldi CLI", info["client_name"])
	assert.Equal(t, true, info["password"])

	step := s.call(http.MethodPost, "/v1/auth/login/password", map[string]any{"login_challenge": challenge, "email": "ada@acme.com", "password": "correct-horse"}, http.StatusOK)
	tokens := redeem(t, cp.URL, s.code(step["redirect_to"].(string)))

	who := whoAmI(t, cp.URL, tokens["access_token"].(string))
	assert.Equal(t, cp.user.ID(), who.Body.PrincipalID)
	assert.Equal(t, cp.org.ID(), who.Body.OrganizationID)

	// Refreshing rotates the refresh token; replaying the old one ends the family.
	refreshed := exchange(t, cp.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {tokens["refresh_token"].(string)}}, http.StatusOK)
	whoAmI(t, cp.URL, refreshed["access_token"].(string))
	replay := exchange(t, cp.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {tokens["refresh_token"].(string)}}, http.StatusBadRequest)
	assert.Equal(t, "invalid_grant", replay["error"])
	exchange(t, cp.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {refreshed["refresh_token"].(string)}}, http.StatusBadRequest)

	// The browser stays signed in: the next authorization skips the frontend.
	sum := sha256.Sum256([]byte(verifier))
	back := s.visit("/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {"ivaldictl"}, "redirect_uri": {cliRedirect}, "state": {"s1"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode())
	assert.Equal(t, "127.0.0.1:9999", back.Host)
}

func TestSignInWithAProviderThroughToAnAPICall(t *testing.T) {
	cp := start(t, false)
	cp.github.Profile = iam.ExternalProfile{Subject: "583231", Email: "admin@acme.com", EmailVerified: true, DisplayName: "Admin", Organizations: []string{"acme"}}
	s := cp.newSession(t)

	next := s.viaGitHub(s.authorize())
	require.True(t, strings.HasSuffix(next.Path, authhttp.ContinuePath), next.String())
	tokens := redeem(t, cp.URL, s.code(next.String()))

	who := whoAmI(t, cp.URL, tokens["access_token"].(string))
	assert.Equal(t, cp.org.ID(), who.Body.OrganizationID)
	assert.NotEqual(t, cp.user.ID(), who.Body.PrincipalID, "a new member, not the password user")
	assert.EqualValues(t, 1, cp.owners(t, who.Body.PrincipalID), "the bootstrap administrator owns the organization")
}

func TestSignUpWithGitHubThroughToAnAPICall(t *testing.T) {
	cp := start(t, true)
	cp.github.Profile = iam.ExternalProfile{Subject: "583231", Email: "grace@example.com", EmailVerified: true, DisplayName: "Grace Hopper"}
	s := cp.newSession(t)
	challenge := s.authorize()

	info := s.call(http.MethodGet, "/v1/auth/login?login_challenge="+url.QueryEscape(challenge), nil, http.StatusOK)
	assert.Equal(t, true, info["sign_up"])
	assert.Equal(t, false, info["password"])

	// A new person comes back to the frontend to name their Organization.
	back := s.viaGitHub(challenge)
	require.Equal(t, "app.example.com", back.Host)
	assert.Equal(t, "grace@example.com", back.Query().Get("email"))
	step := s.call(http.MethodPost, "/v1/auth/signup", map[string]any{
		"login_challenge": challenge, "signup_token": back.Query().Get("signup_token"), "organization_name": "Compilers Inc",
	}, http.StatusOK)

	tokens := redeem(t, cp.URL, s.code(step["redirect_to"].(string)))
	who := whoAmI(t, cp.URL, tokens["access_token"].(string))
	var slug string
	require.NoError(t, cp.db.Raw("SELECT slug FROM iam.organizations WHERE id = ?", who.Body.OrganizationID).Scan(&slug).Error)
	assert.Equal(t, "compilers-inc", slug)
	assert.EqualValues(t, 1, cp.owners(t, who.Body.PrincipalID), "the founder owns the new organization")

	// Signing in again in a new browser finds the account, without signing up.
	again := cp.newSession(t)
	next := again.viaGitHub(again.authorize())
	assert.True(t, strings.HasSuffix(next.Path, authhttp.ContinuePath), next.String())
	var accounts int64
	require.NoError(t, cp.db.Raw("SELECT count(*) FROM iam.accounts").Scan(&accounts).Error)
	assert.EqualValues(t, 1, accounts)
}

func TestAPIRefusesRequestsWithoutAToken(t *testing.T) {
	cp := start(t, false)

	resp, err := http.Get(cp.URL + "/v1/whoami")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), cp.URL+"/.well-known/oauth-protected-resource")

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/.well-known/jwks.json"} {
		resp, err := http.Get(cp.URL + path)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
}

// provisioned records the Organizations onboarding provisions, standing in
// for the workspace capability.
type provisioned struct{ orgs []string }

func (p *provisioned) ProvisionOrganization(_ context.Context, organizationID string) error {
	p.orgs = append(p.orgs, organizationID)
	return nil
}

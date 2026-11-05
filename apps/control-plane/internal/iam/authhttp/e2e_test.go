package authhttp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
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

// controlPlane is a running control plane over a throwaway database.
type controlPlane struct {
	*httptest.Server
	db       *gorm.DB
	org      *iam.Organization
	user     *iam.User
	provider *connectortest.Fake
}

// startControlPlane wires the real sign-in, authorization server, pages, and
// API, as serve does, with GitHub sign-in through a fake connector.
func startControlPlane(t *testing.T) (*httptest.Server, *iam.User) {
	t.Helper()
	cp := startFullControlPlane(t)
	return cp.Server, cp.user
}

func startFullControlPlane(t *testing.T) *controlPlane {
	t.Helper()
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS)
	srv := httptest.NewUnstartedServer(nil)
	issuer := "http://" + srv.Listener.Addr().String()

	hasher := hashertest.NewFake()
	org := mustDo(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, nil))
	require.NoError(t, iampg.NewOrganizationRepository(db).Write(ctx, org))
	account := mustDo(iam.NewAccount(iam.NewAccountProps{Email: "ada@acme.com", DisplayName: "Ada"}, nil))
	require.NoError(t, iampg.NewAccountRepository(db).Write(ctx, account))
	hash, _ := hasher.Hash("correct-horse")
	require.NoError(t, iampg.NewPasswordCredentialRepository(db).Write(ctx, mustDo(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: account.ID(), PasswordHash: hash}))))
	user := mustDo(iam.NewUser(iam.NewUserProps{PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Ada"}, AccountID: account.ID()}, nil))
	require.NoError(t, iampg.NewUserRepository(db).Write(ctx, user))
	require.NoError(t, iampg.NewOAuthClientRepository(db).Write(ctx, mustDo(iam.NewOAuthClient(iam.NewOAuthClientProps{
		ClientID: "ivaldictl", Registration: iam.RegistrationFirstParty, Name: "Ivaldi CLI", RedirectURIs: []string{"http://127.0.0.1/callback"},
	}, nil))))
	require.NoError(t, iampg.NewResourceRegistrar(db).RegisterResource(ctx, org.Resource()))
	github := mustDo(iam.NewIdentityProvider(iam.NewIdentityProviderProps{
		OrganizationID: org.ID(), Protocol: iam.ProtocolGitHub, Name: "GitHub",
		Config:                 iam.IdentityProviderConfig{ClientID: "Iv1.abc", AllowedOrganizations: []string{"acme"}},
		ManagedByConfiguration: true, JITProvisioning: true,
	}, nil))
	require.NoError(t, iampg.NewIdentityProviderRepository(db).Write(ctx, github))
	fakeGitHub := &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub"}

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
		signin.WithGrantRepo(iampg.NewGrantRepository(db)),
		signin.WithPasswordHasher(hasher),
		signin.WithTransactor(tx),
		signin.WithOutboxRecorder(nopOutbox{}),
		signin.WithBuiltinAccounts(true),
		signin.WithBootstrapAdminEmail("admin@acme.com"),
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
	codec := mustDo(session.NewCodec(mustDo(keys.DeriveKeys(session.KeyPurpose))))
	pages := mustDo(authhttp.New(
		authhttp.WithSettings(authhttp.Settings{Issuer: issuer, SessionTTL: 10 * time.Minute, BuiltinAccounts: true}),
		authhttp.WithLogger(logger), authhttp.WithAuthentication(auth), authhttp.WithAuthorizationServer(server), authhttp.WithCodec(codec),
		authhttp.WithProviders(authhttp.Provider{Kind: "github", Connector: fakeGitHub, OrganizationID: org.ID(), IdentityProviderID: github.ID()}),
	))
	v := mustDo(identity.NewVerifier(identity.VerifierConfig{Issuer: issuer, Audience: issuer, Keys: identity.NewStaticKeySource(keys.PublicKeys()...)}))

	port := 0
	apiServer := mustDo(api.New(&config.Config{Port: &port, ExternalURL: issuer}, "test", logger,
		api.WithRoutes(whoami{}), api.WithHandlers(authhttp.NewWellKnown(keys), pages), api.WithVerifier(v)))
	srv.Config.Handler = apiServer.Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	return &controlPlane{Server: srv, db: db, org: org, user: user, provider: fakeGitHub}
}

func mustDo[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func TestSignInWithAPasswordThroughToAnAPICall(t *testing.T) {
	srv, user := startControlPlane(t)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// ivaldictl opens the browser at the authorization endpoint.
	sum := sha256.Sum256([]byte(verifier))
	authorize := srv.URL + "/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {"ivaldictl"}, "redirect_uri": {cliRedirect}, "state": {"s1"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode()
	resp, err := browser.Get(authorize)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	login := srv.URL + resp.Header.Get("Location")
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?"))

	// The person signs in.
	resp, err = browser.Get(login)
	require.NoError(t, err)
	page, _ := io.ReadAll(resp.Body)
	csrf := csrfField.FindStringSubmatch(string(page))
	require.NotNil(t, csrf)
	returnTo := mustDo(url.Parse(login)).Query().Get("return_to")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/login", strings.NewReader(url.Values{
		"csrf": {csrf[1]}, "email": {"ada@acme.com"}, "password": {"correct-horse"}, "return_to": {returnTo},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	resp, err = browser.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	// Back at the authorization endpoint, signed in: a code for the CLI.
	resp, err = browser.Get(srv.URL + resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	callback := mustDo(url.Parse(resp.Header.Get("Location")))
	assert.Equal(t, "127.0.0.1:9999", callback.Host)
	assert.Equal(t, "s1", callback.Query().Get("state"))
	assert.Equal(t, srv.URL, callback.Query().Get("iss"))

	// The CLI exchanges the code.
	tokens := exchange(t, srv.URL, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"ivaldictl"}, "code": {callback.Query().Get("code")},
		"redirect_uri": {cliRedirect}, "code_verifier": {verifier},
	}, http.StatusOK)

	// And calls the API.
	who := whoAmI(t, srv.URL, tokens["access_token"].(string))
	assert.Equal(t, user.ID(), who.Body.PrincipalID)
	assert.Equal(t, user.OrganizationID(), who.Body.OrganizationID)

	// Refreshing rotates the refresh token; replaying the old one ends the family.
	refreshed := exchange(t, srv.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {tokens["refresh_token"].(string)}}, http.StatusOK)
	whoAmI(t, srv.URL, refreshed["access_token"].(string))
	replay := exchange(t, srv.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {tokens["refresh_token"].(string)}}, http.StatusBadRequest)
	assert.Equal(t, "invalid_grant", replay["error"])
	exchange(t, srv.URL, url.Values{"grant_type": {"refresh_token"}, "client_id": {"ivaldictl"}, "refresh_token": {refreshed["refresh_token"].(string)}}, http.StatusBadRequest)
}

func TestAPIRefusesRequestsWithoutAToken(t *testing.T) {
	srv, _ := startControlPlane(t)

	resp, err := http.Get(srv.URL + "/v1/whoami")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), srv.URL+"/.well-known/oauth-protected-resource")

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/.well-known/jwks.json"} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
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

func TestSignInWithAProviderThroughToAnAPICall(t *testing.T) {
	cp := startFullControlPlane(t)
	cp.provider.Profile = iam.ExternalProfile{Subject: "583231", Email: "admin@acme.com", EmailVerified: true, DisplayName: "Admin", Organizations: []string{"acme"}}
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sum := sha256.Sum256([]byte(verifier))
	authorize := cp.URL + "/oauth/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {"ivaldictl"}, "redirect_uri": {cliRedirect}, "state": {"s2"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode()

	// Not signed in: to the login page, then "Continue with GitHub".
	resp, err := browser.Get(authorize)
	require.NoError(t, err)
	returnTo := mustDo(url.Parse(resp.Header.Get("Location"))).Query().Get("return_to")
	resp, err = browser.Get(cp.URL + "/auth/oauth/github/start?" + url.Values{"return_to": {returnTo}}.Encode())
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	state := mustDo(url.Parse(resp.Header.Get("Location"))).Query().Get("state")

	// GitHub sends the browser back; the administrator is provisioned and claims ownership.
	resp, err = browser.Get(cp.URL + "/auth/oauth/github/callback?" + url.Values{"code": {"gh-code"}, "state": {state}}.Encode())
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	resp, err = browser.Get(cp.URL + resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	code := mustDo(url.Parse(resp.Header.Get("Location"))).Query().Get("code")
	require.NotEmpty(t, code)

	tokens := exchange(t, cp.URL, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"ivaldictl"}, "code": {code},
		"redirect_uri": {cliRedirect}, "code_verifier": {verifier},
	}, http.StatusOK)
	who := whoAmI(t, cp.URL, tokens["access_token"].(string))
	assert.Equal(t, cp.org.ID(), who.Body.OrganizationID)
	assert.NotEqual(t, cp.user.ID(), who.Body.PrincipalID, "a new member, not the password user")

	var owners int64
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	require.NoError(t, cp.db.Raw("SELECT count(*) FROM iam.grants WHERE role_id = ? AND principal_id = ?", ownerRole, who.Body.PrincipalID).Scan(&owners).Error)
	assert.EqualValues(t, 1, owners, "the bootstrap administrator owns the organization")
}

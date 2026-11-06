package oauthserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher/hashertest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/signin"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const (
	issuerURL = "https://ivaldi.example.com"
	redirect  = "http://127.0.0.1:53123/callback"
	verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func challenge(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

type nopOutbox struct{}

func (nopOutbox) Record(context.Context, ...events.Envelope) error { return nil }

type fixture struct {
	ctx      context.Context
	db       *gorm.DB
	server   iam.AuthorizationServer
	verify   identity.Verifier
	org      *iam.Organization
	user     *iam.User
	member   iam.Membership
	cli      *iam.OAuthClient
	thirdPty *iam.OAuthClient
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS)
	f := &fixture{ctx: ctx, db: db}

	f.org = must(iam.NewOrganization(iam.NewOrganizationProps{Slug: "acme", DisplayName: "Acme"}, nil))
	require.NoError(t, iampg.NewOrganizationRepository(db).Write(ctx, f.org))
	account := must(iam.NewAccount(iam.NewAccountProps{Email: "ada@acme.com", DisplayName: "Ada"}, nil))
	require.NoError(t, iampg.NewAccountRepository(db).Write(ctx, account))
	f.user = must(iam.NewUser(iam.NewUserProps{PrincipalProps: iam.PrincipalProps{OrganizationID: f.org.ID(), DisplayName: "Ada"}, AccountID: account.ID()}, nil))
	require.NoError(t, iampg.NewUserRepository(db).Write(ctx, f.user))
	f.member = iam.Membership{OrganizationID: f.org.ID(), PrincipalID: f.user.ID()}

	clients := iampg.NewOAuthClientRepository(db)
	f.cli = must(iam.NewOAuthClient(iam.NewOAuthClientProps{ClientID: "ivaldictl", Registration: iam.RegistrationFirstParty, Name: "Ivaldi CLI", RedirectURIs: []string{"http://127.0.0.1/callback"}}, nil))
	require.NoError(t, clients.Write(ctx, f.cli))
	f.thirdPty = must(iam.NewOAuthClient(iam.NewOAuthClientProps{Registration: iam.RegistrationDynamic, Name: "Editor", RedirectURIs: []string{"http://127.0.0.1/callback"}}, nil))
	require.NoError(t, clients.Write(ctx, f.thirdPty))

	keys, err := token.NewEphemeralKeyRing()
	require.NoError(t, err)
	issuer, err := token.NewIssuer(keys, issuerURL, 15*time.Minute)
	require.NoError(t, err)
	f.verify, err = identity.NewVerifier(identity.VerifierConfig{
		Issuer: issuerURL, Audience: issuerURL, Keys: identity.NewStaticKeySource(keys.PublicKeys()...), Now: clock.Now,
	})
	require.NoError(t, err)

	logger := logging.NewMockLogger()
	logger.On("Warn", mock.Anything, mock.Anything, mock.Anything).Maybe()
	logger.On("Error", mock.Anything, mock.Anything).Maybe()
	tx := database.NewTransactor(db)
	auth, err := signin.New(
		signin.WithLogger(logger),
		signin.WithOrganizationRepo(iampg.NewOrganizationRepository(db)),
		signin.WithAccountRepo(iampg.NewAccountRepository(db)),
		signin.WithPasswordCredentialRepo(iampg.NewPasswordCredentialRepository(db)),
		signin.WithUserRepo(iampg.NewUserRepository(db)),
		signin.WithIdentityProviderRepo(iampg.NewIdentityProviderRepository(db)),
		signin.WithExternalIdentityRepo(iampg.NewExternalIdentityRepository(db)),
		signin.WithAccountIdentityRepo(iampg.NewAccountIdentityRepository(db)),
		signin.WithGrantRepo(iampg.NewGrantRepository(db)),
		signin.WithPasswordHasher(hashertest.NewFake()),
		signin.WithTransactor(tx),
		signin.WithOutboxRecorder(nopOutbox{}),
	)
	require.NoError(t, err)
	f.server, err = New(
		WithSettings(Settings{Issuer: issuerURL, Resources: []string{issuerURL}, Scopes: []string{"read"}, CodeTTL: time.Minute, RefreshTTL: 24 * time.Hour, AccessTTL: 15 * time.Minute}),
		WithLogger(logger),
		WithIssuer(issuer),
		WithAuthentication(auth),
		WithOAuthClientRepo(clients),
		WithAuthorizationCodeRepo(iampg.NewOAuthAuthorizationCodeRepository(db)),
		WithRefreshTokenRepo(iampg.NewOAuthRefreshTokenRepository(db)),
		WithUserRepo(iampg.NewUserRepository(db)),
		WithTransactor(tx),
		WithOutboxRecorder(nopOutbox{}),
	)
	require.NoError(t, err)
	return f
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func authorize() iam.AuthorizeRequest {
	return iam.AuthorizeRequest{
		ResponseType: "code", ClientID: "ivaldictl", RedirectURI: redirect, State: "xyz",
		CodeChallenge: challenge(verifier), CodeChallengeMethod: "S256", Scope: "read write",
	}
}

// code runs an authorization through to a code.
func (f *fixture) code(t *testing.T) string {
	t.Helper()
	v, err := f.server.ValidateAuthorization(f.ctx, authorize())
	require.NoError(t, err)
	location, err := f.server.IssueCode(f.ctx, v, f.member)
	require.NoError(t, err)
	u, err := url.Parse(location)
	require.NoError(t, err)
	return u.Query().Get("code")
}

func (f *fixture) exchange(t *testing.T, code string) (*iam.TokenResponse, error) {
	t.Helper()
	return f.server.Exchange(f.ctx, iam.TokenRequest{GrantType: "authorization_code", ClientID: "ivaldictl", Code: code, RedirectURI: redirect, CodeVerifier: verifier})
}

func (f *fixture) refresh(refreshToken string) (*iam.TokenResponse, error) {
	return f.server.Exchange(f.ctx, iam.TokenRequest{GrantType: "refresh_token", ClientID: "ivaldictl", RefreshToken: refreshToken})
}

func oauthCode(t *testing.T, err error) *iam.OAuthError {
	t.Helper()
	var oe *iam.OAuthError
	require.True(t, errors.As(err, &oe), "want an OAuthError, got %v", err)
	return oe
}

func TestValidateAuthorization(t *testing.T) {
	f := newFixture(t)
	with := func(change func(*iam.AuthorizeRequest)) iam.AuthorizeRequest {
		r := authorize()
		change(&r)
		return r
	}

	t.Run("valid", func(t *testing.T) {
		v, err := f.server.ValidateAuthorization(f.ctx, authorize())
		require.NoError(t, err)
		assert.Equal(t, f.cli.ID(), v.Client)
		assert.Equal(t, issuerURL, v.Resource, "the resource defaults to the API")
		assert.Equal(t, []string{"read"}, v.Scopes, "unknown scopes are dropped")
	})

	shownToThePerson := []struct {
		name string
		req  iam.AuthorizeRequest
	}{
		{"unknown client", with(func(r *iam.AuthorizeRequest) { r.ClientID = "nope" })},
		{"no redirect URI", with(func(r *iam.AuthorizeRequest) { r.RedirectURI = "" })},
		{"unregistered redirect URI", with(func(r *iam.AuthorizeRequest) { r.RedirectURI = "https://evil.example.com/cb" })},
	}
	for _, tt := range shownToThePerson {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.server.ValidateAuthorization(f.ctx, tt.req)
			oe := oauthCode(t, err)
			assert.Empty(t, oe.RedirectURI, "never redirected to a URL that was not verified")
		})
	}

	sentToTheClient := []struct {
		name string
		req  iam.AuthorizeRequest
		code string
	}{
		{"implicit flow", with(func(r *iam.AuthorizeRequest) { r.ResponseType = "token" }), iam.OAuthUnsupportedResponseType},
		{"no PKCE", with(func(r *iam.AuthorizeRequest) { r.CodeChallenge = "" }), iam.OAuthInvalidRequest},
		{"plain PKCE", with(func(r *iam.AuthorizeRequest) { r.CodeChallengeMethod = "plain" }), iam.OAuthInvalidRequest},
		{"another resource", with(func(r *iam.AuthorizeRequest) { r.Resource = "https://other.example.com" }), iam.OAuthInvalidTarget},
		{"a client that needs consent", with(func(r *iam.AuthorizeRequest) { r.ClientID = f.thirdPty.ClientID() }), iam.OAuthUnauthorizedClient},
	}
	for _, tt := range sentToTheClient {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.server.ValidateAuthorization(f.ctx, tt.req)
			oe := oauthCode(t, err)
			assert.Equal(t, tt.code, oe.Code)
			location, err := url.Parse(oe.Location(issuerURL))
			require.NoError(t, err)
			assert.Equal(t, "127.0.0.1:53123", location.Host)
			assert.Equal(t, tt.code, location.Query().Get("error"))
			assert.Equal(t, "xyz", location.Query().Get("state"))
			assert.Equal(t, issuerURL, location.Query().Get("iss"))
		})
	}
}

func TestCodeFlow(t *testing.T) {
	f := newFixture(t)

	v, err := f.server.ValidateAuthorization(f.ctx, authorize())
	require.NoError(t, err)
	location, err := f.server.IssueCode(f.ctx, v, f.member)
	require.NoError(t, err)
	u, err := url.Parse(location)
	require.NoError(t, err)
	assert.Equal(t, "xyz", u.Query().Get("state"))
	assert.Equal(t, issuerURL, u.Query().Get("iss"), "the client can tell which server answered (RFC 9207)")
	code := u.Query().Get("code")
	assert.Len(t, code, 43)
	assert.Zero(t, countRows(t, f.db, "SELECT count(*) FROM iam.oauth_authorization_codes WHERE code_hash = ?", []byte(code)), "only the hash is stored")

	resp, err := f.exchange(t, code)
	require.NoError(t, err)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, 900, resp.ExpiresIn)
	assert.Equal(t, "read", resp.Scope)
	assert.True(t, strings.HasPrefix(resp.RefreshToken, "ivr_"))

	claims, err := f.verify.Verify(f.ctx, resp.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, f.user.ID(), claims.Subject)
	assert.Equal(t, f.org.ID(), claims.OrganizationID)
	assert.Equal(t, "ivaldictl", claims.ClientID)
	assert.Equal(t, []string{"read"}, claims.Scopes)

	_, err = f.exchange(t, code)
	assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code, "a code works once")
}

func TestCodeExchangeRefuses(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name string
		req  func(code string) iam.TokenRequest
		want string
	}{
		{"wrong verifier", func(c string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "authorization_code", ClientID: "ivaldictl", Code: c, RedirectURI: redirect, CodeVerifier: strings.Repeat("a", 43)}
		}, iam.OAuthInvalidGrant},
		{"another redirect URI", func(c string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "authorization_code", ClientID: "ivaldictl", Code: c, RedirectURI: "http://127.0.0.1:1/callback", CodeVerifier: verifier}
		}, iam.OAuthInvalidGrant},
		{"another client", func(c string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "authorization_code", ClientID: f.thirdPty.ClientID(), Code: c, RedirectURI: redirect, CodeVerifier: verifier}
		}, iam.OAuthInvalidGrant},
		{"unknown client", func(c string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "authorization_code", ClientID: "nope", Code: c, RedirectURI: redirect, CodeVerifier: verifier}
		}, iam.OAuthInvalidClient},
		{"unknown code", func(string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "authorization_code", ClientID: "ivaldictl", Code: "forged", RedirectURI: redirect, CodeVerifier: verifier}
		}, iam.OAuthInvalidGrant},
		{"unsupported grant", func(string) iam.TokenRequest {
			return iam.TokenRequest{GrantType: "password", ClientID: "ivaldictl"}
		}, iam.OAuthUnsupportedGrantType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.server.Exchange(f.ctx, tt.req(f.code(t)))
			assert.Equal(t, tt.want, oauthCode(t, err).Code)
		})
	}

	t.Run("expired code", func(t *testing.T) {
		c := f.code(t)
		clock.Fix(t, time.Now().Add(2*time.Minute))
		_, err := f.exchange(t, c)
		assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code)
	})
}

func TestRefreshRotatesAndReplayRevokesTheFamily(t *testing.T) {
	f := newFixture(t)
	first, err := f.exchange(t, f.code(t))
	require.NoError(t, err)

	second, err := f.refresh(first.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken)
	_, err = f.verify.Verify(f.ctx, second.AccessToken)
	require.NoError(t, err)

	third, err := f.refresh(second.RefreshToken)
	require.NoError(t, err)

	_, err = f.refresh(first.RefreshToken)
	assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code, "a used refresh token is a replay")

	_, err = f.refresh(third.RefreshToken)
	assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code, "the replay revoked the whole family, including the newest token")
	assert.Zero(t, countRows(t, f.db, "SELECT count(*) FROM iam.oauth_refresh_tokens WHERE revoked_at IS NULL"), "the revocation was committed although the request failed")
}

func TestRefreshRefusesASuspendedMember(t *testing.T) {
	f := newFixture(t)
	resp, err := f.exchange(t, f.code(t))
	require.NoError(t, err)
	require.NoError(t, f.user.Suspend())
	require.NoError(t, iampg.NewUserRepository(f.db).Write(f.ctx, f.user))

	_, err = f.refresh(resp.RefreshToken)

	assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code)
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	resp, err := f.exchange(t, f.code(t))
	require.NoError(t, err)

	require.NoError(t, f.server.Revoke(f.ctx, "unknown", "ivaldictl"), "an unknown token is not an error")
	require.NoError(t, f.server.Revoke(f.ctx, resp.RefreshToken, f.thirdPty.ClientID()))
	_, err = f.refresh(resp.RefreshToken)
	require.NoError(t, err, "another client cannot revoke the token")

	fresh, err := f.exchange(t, f.code(t))
	require.NoError(t, err)
	require.NoError(t, f.server.Revoke(f.ctx, fresh.RefreshToken, "ivaldictl"))
	_, err = f.refresh(fresh.RefreshToken)
	assert.Equal(t, iam.OAuthInvalidGrant, oauthCode(t, err).Code)
}

func TestMetadata(t *testing.T) {
	m := newFixture(t).server.Metadata()

	assert.Equal(t, issuerURL, m.Issuer)
	assert.Equal(t, issuerURL+"/oauth/authorize", m.AuthorizationEndpoint)
	assert.Equal(t, issuerURL+"/oauth/token", m.TokenEndpoint)
	assert.Equal(t, issuerURL+"/.well-known/jwks.json", m.JWKSURI)
	assert.Equal(t, []string{"S256"}, m.CodeChallengeMethodsSupported)
	assert.Equal(t, []string{"none"}, m.TokenEndpointAuthMethodsSupported, "every client is public and uses PKCE")
	assert.True(t, m.AuthorizationResponseIssParameterSupported)
}

func countRows(t *testing.T, db *gorm.DB, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

func TestCodeExchangeRecordsTheSignIn(t *testing.T) {
	f := newFixture(t)
	signedIn := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, signedIn)
	users := iampg.NewUserRepository(f.db)

	resp, err := f.exchange(t, f.code(t))
	require.NoError(t, err)
	u, err := users.Get(f.ctx, f.org.ID(), f.user.ID())
	require.NoError(t, err)
	assert.Equal(t, &signedIn, u.LastSignedInAt())

	clock.Fix(t, signedIn.Add(time.Minute))
	_, err = f.refresh(resp.RefreshToken)
	require.NoError(t, err)
	u, err = users.Get(f.ctx, f.org.ID(), f.user.ID())
	require.NoError(t, err)
	assert.Equal(t, &signedIn, u.LastSignedInAt(), "refreshing is not signing in")
}

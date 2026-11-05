// Package oauthserver implements iam.AuthorizationServer: OAuth 2.1's
// authorization code grant with PKCE, and refresh tokens that rotate on
// every use and revoke their whole family when a used one is replayed.
package oauthserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// refreshTokenPrefix marks Ivaldi refresh tokens, so secret scanners can
// recognize a leaked one.
const refreshTokenPrefix = "ivr_"

// pkceChallenge is an S256 code challenge: 43 base64url characters, or up to
// 128 for clients that send longer ones.
var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)

// Settings configure the server.
type Settings struct {
	// Issuer is the control plane's external URL.
	Issuer string
	// Resources are the resource servers tokens may be issued for; the
	// first is the default.
	Resources []string
	// Scopes are the scopes the server knows. Others are dropped.
	Scopes     []string
	CodeTTL    time.Duration
	RefreshTTL time.Duration
	// AccessTTL is reported to clients as expires_in.
	AccessTTL time.Duration
}

// Option configures New.
type Option func(*server)

func WithSettings(s Settings) Option     { return func(v *server) { v.settings = s } }
func WithLogger(l logging.Logger) Option { return func(v *server) { v.logger = l } }
func WithIssuer(i token.Issuer) Option   { return func(v *server) { v.issuer = i } }
func WithAuthentication(a iam.Authentication) Option {
	return func(v *server) { v.auth = a }
}
func WithOAuthClientRepo(r iam.OAuthClientRepository) Option {
	return func(v *server) { v.clients = r }
}
func WithAuthorizationCodeRepo(r iam.OAuthAuthorizationCodeRepository) Option {
	return func(v *server) { v.codes = r }
}
func WithRefreshTokenRepo(r iam.OAuthRefreshTokenRepository) Option {
	return func(v *server) { v.refreshes = r }
}
func WithUserRepo(r iam.UserRepository) Option        { return func(v *server) { v.users = r } }
func WithTransactor(tx transaction.Transactor) Option { return func(v *server) { v.tx = tx } }
func WithOutboxRecorder(o outbox.Recorder) Option     { return func(v *server) { v.outbox = o } }

// WithRandom replaces the source of codes and refresh tokens, for tests.
func WithRandom(r io.Reader) Option { return func(v *server) { v.random = r } }

type server struct {
	settings  Settings
	logger    logging.Logger
	issuer    token.Issuer
	auth      iam.Authentication
	clients   iam.OAuthClientRepository
	codes     iam.OAuthAuthorizationCodeRepository
	refreshes iam.OAuthRefreshTokenRepository
	users     iam.UserRepository
	tx        transaction.Transactor
	outbox    outbox.Recorder
	random    io.Reader
}

// New returns the AuthorizationServer, or an error naming a missing
// dependency or setting.
func New(opts ...Option) (iam.AuthorizationServer, error) {
	s := &server{random: rand.Reader}
	for _, o := range opts {
		o(s)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Logger", s.logger != nil},
		{"Issuer", s.issuer != nil},
		{"Authentication", s.auth != nil},
		{"OAuthClientRepo", s.clients != nil},
		{"AuthorizationCodeRepo", s.codes != nil},
		{"RefreshTokenRepo", s.refreshes != nil},
		{"UserRepo", s.users != nil},
		{"Transactor", s.tx != nil},
		{"Outbox", s.outbox != nil},
		{"Settings.Issuer", s.settings.Issuer != ""},
		{"Settings.Resources", len(s.settings.Resources) > 0},
		{"Settings.CodeTTL", s.settings.CodeTTL > 0},
		{"Settings.RefreshTTL", s.settings.RefreshTTL > 0},
		{"Settings.AccessTTL", s.settings.AccessTTL > 0},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("oauthserver: %s is required", r.name)
		}
	}
	return s, nil
}

func (s *server) ValidateAuthorization(ctx context.Context, req iam.AuthorizeRequest) (*iam.ValidatedAuthorization, error) {
	// Until the client and its redirect URI check out, errors go to the
	// person, never to a URL an attacker may have chosen.
	client, err := s.clients.GetByClientID(ctx, req.ClientID)
	if errors.Is(err, iam.ErrNotFound) || req.ClientID == "" {
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "unknown client"}
	}
	if err != nil {
		return nil, err
	}
	if req.RedirectURI == "" || !client.AllowsRedirect(req.RedirectURI) {
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "the redirect URI is not registered for this client"}
	}

	fail := func(code, description string) error {
		return &iam.OAuthError{Code: code, Description: description, RedirectURI: req.RedirectURI, State: req.State}
	}
	if req.ResponseType != "code" {
		return nil, fail(iam.OAuthUnsupportedResponseType, "only the code response type is supported")
	}
	// Other clients need the person's consent, which is not built yet.
	if !client.FirstParty() {
		return nil, fail(iam.OAuthUnauthorizedClient, "this client is not allowed to request authorization")
	}
	if req.CodeChallengeMethod != "S256" || !pkceChallenge.MatchString(req.CodeChallenge) {
		return nil, fail(iam.OAuthInvalidRequest, "an S256 PKCE code challenge is required")
	}
	resource, err := s.resource(req.Resource)
	if err != nil {
		return nil, fail(iam.OAuthInvalidTarget, err.Error())
	}
	return &iam.ValidatedAuthorization{
		Client:        client.ID(),
		ClientID:      client.ClientID(),
		ClientName:    client.Name(),
		RedirectURI:   req.RedirectURI,
		State:         req.State,
		CodeChallenge: req.CodeChallenge,
		Resource:      resource,
		Scopes:        s.knownScopes(req.Scope),
	}, nil
}

func (s *server) IssueCode(ctx context.Context, v *iam.ValidatedAuthorization, m iam.Membership) (string, error) {
	code, err := s.secret()
	if err != nil {
		return "", err
	}
	c, err := iam.NewOAuthAuthorizationCode(iam.NewOAuthAuthorizationCodeProps{
		CodeHash:       hash(code),
		OrganizationID: m.OrganizationID,
		PrincipalID:    m.PrincipalID,
		ClientID:       v.Client,
		RedirectURI:    v.RedirectURI,
		CodeChallenge:  v.CodeChallenge,
		Resource:       v.Resource,
		Scopes:         v.Scopes,
		ExpiresAt:      clock.Now().Add(s.settings.CodeTTL),
	})
	if err != nil {
		return "", err
	}
	if err := s.codes.Write(ctx, c); err != nil {
		return "", fmt.Errorf("write authorization code: %w", err)
	}
	q := url.Values{"code": {code}, "iss": {s.settings.Issuer}}
	if v.State != "" {
		q.Set("state", v.State)
	}
	return iam.AppendQuery(v.RedirectURI, q), nil
}

func (s *server) Exchange(ctx context.Context, req iam.TokenRequest) (*iam.TokenResponse, error) {
	client, err := s.clients.GetByClientID(ctx, req.ClientID)
	if errors.Is(err, iam.ErrNotFound) || req.ClientID == "" {
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidClient, Description: "unknown client"}
	}
	if err != nil {
		return nil, err
	}
	switch req.GrantType {
	case "authorization_code":
		return s.exchangeCode(ctx, client, req)
	case "refresh_token":
		return s.refresh(ctx, client, req)
	}
	return nil, &iam.OAuthError{Code: iam.OAuthUnsupportedGrantType, Description: "use authorization_code or refresh_token"}
}

func (s *server) exchangeCode(ctx context.Context, client *iam.OAuthClient, req iam.TokenRequest) (*iam.TokenResponse, error) {
	if req.Code == "" || req.CodeVerifier == "" {
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "code and code_verifier are required"}
	}
	var resp *iam.TokenResponse
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		code, err := s.codes.GetForUpdate(ctx, hash(req.Code))
		if errors.Is(err, iam.ErrNotFound) {
			return invalidGrant("unknown authorization code")
		}
		if err != nil {
			return err
		}
		if err := code.Redeem(client.ID(), req.RedirectURI, req.CodeVerifier); err != nil {
			return grantError(err)
		}
		if err := s.codes.Write(ctx, code); err != nil {
			return err
		}
		codeID := base64.RawURLEncoding.EncodeToString(code.CodeHash())
		if err := s.record(ctx, code.OrganizationID(), iam.AggregateAuthorizationCode, codeID, code); err != nil {
			return err
		}
		if req.Resource != "" && req.Resource != code.Resource() {
			return &iam.OAuthError{Code: iam.OAuthInvalidTarget, Description: "the resource must be the one authorized"}
		}
		if err := s.checkMembership(ctx, code.OrganizationID(), code.PrincipalID()); err != nil {
			return err
		}

		refresh, err := s.secret()
		if err != nil {
			return err
		}
		rt, err := iam.NewOAuthRefreshToken(iam.NewOAuthRefreshTokenProps{
			OrganizationID: code.OrganizationID(),
			PrincipalID:    code.PrincipalID(),
			ClientID:       client.ID(),
			TokenHash:      hash(refreshTokenPrefix + refresh),
			Resource:       code.Resource(),
			Scopes:         code.Scopes(),
			ExpiresAt:      clock.Now().Add(s.settings.RefreshTTL),
		}, nil)
		if err != nil {
			return err
		}
		if err := s.refreshes.Write(ctx, rt); err != nil {
			return err
		}
		if err := s.record(ctx, rt.OrganizationID(), iam.AggregateRefreshToken, rt.ID(), rt); err != nil {
			return err
		}
		resp, err = s.respond(ctx, client, rt, refreshTokenPrefix+refresh)
		return err
	})
	if err != nil {
		return nil, s.oauthError(err)
	}
	return resp, nil
}

// errReplayed means a refresh token that was already rotated came back:
// whoever holds the family's tokens may have been robbed of one.
var errReplayed = errors.New("refresh token replayed")

func (s *server) refresh(ctx context.Context, client *iam.OAuthClient, req iam.TokenRequest) (*iam.TokenResponse, error) {
	if req.RefreshToken == "" {
		return nil, &iam.OAuthError{Code: iam.OAuthInvalidRequest, Description: "refresh_token is required"}
	}
	var resp *iam.TokenResponse
	var replayed *iam.OAuthRefreshToken
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		old, err := s.refreshes.GetByHashForUpdate(ctx, hash(req.RefreshToken))
		if errors.Is(err, iam.ErrNotFound) {
			return invalidGrant("unknown refresh token")
		}
		if err != nil {
			return err
		}
		if req.Resource != "" && req.Resource != old.Resource() {
			return &iam.OAuthError{Code: iam.OAuthInvalidTarget, Description: "the resource must be the one authorized"}
		}
		if req.Scope != "" {
			for _, sc := range strings.Fields(req.Scope) {
				if !slices.Contains(old.Scopes(), sc) {
					return &iam.OAuthError{Code: iam.OAuthInvalidScope, Description: "a refresh cannot add scopes"}
				}
			}
		}

		secret, err := s.secret()
		if err != nil {
			return err
		}
		value := refreshTokenPrefix + secret
		// The family keeps the first token's expiry: rotating does not
		// extend how long a sign-in lasts.
		next, err := old.Rotate(client.ID(), hash(value), old.ExpiresAt())
		if errors.Is(err, iam.ErrAlreadyUsed) {
			replayed = old
			return errReplayed
		}
		if err != nil {
			return grantError(err)
		}
		if err := s.checkMembership(ctx, old.OrganizationID(), old.PrincipalID()); err != nil {
			return err
		}
		if err := s.refreshes.Write(ctx, next); err != nil {
			return err
		}
		if err := s.refreshes.Write(ctx, old); err != nil {
			return err
		}
		if err := s.record(ctx, old.OrganizationID(), iam.AggregateRefreshToken, old.ID(), old); err != nil {
			return err
		}
		if err := s.record(ctx, next.OrganizationID(), iam.AggregateRefreshToken, next.ID(), next); err != nil {
			return err
		}
		resp, err = s.respond(ctx, client, next, value)
		return err
	})
	if errors.Is(err, errReplayed) {
		// Revoked in its own transaction, so the revocation stands although
		// this request fails.
		if err := s.revokeFamily(ctx, replayed.OrganizationID(), replayed.FamilyID()); err != nil {
			return nil, err
		}
		s.logger.Warn("refresh token replayed; revoked its family",
			logging.F("organization_id", replayed.OrganizationID()), logging.F("family_id", replayed.FamilyID()))
		return nil, invalidGrant("the refresh token was already used")
	}
	if err != nil {
		return nil, s.oauthError(err)
	}
	return resp, nil
}

func (s *server) Revoke(ctx context.Context, tok, clientID string) error {
	rt, err := s.refreshes.GetByHashForUpdate(ctx, hash(tok))
	if errors.Is(err, iam.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	client, err := s.clients.GetByClientID(ctx, clientID)
	if errors.Is(err, iam.ErrNotFound) || (err == nil && client.ID() != rt.ClientID()) {
		// RFC 7009: a token another client holds is not revoked, and the
		// caller is not told so.
		return nil
	}
	if err != nil {
		return err
	}
	return s.revokeFamily(ctx, rt.OrganizationID(), rt.FamilyID())
}

func (s *server) revokeFamily(ctx context.Context, organizationID, familyID string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		tokens, err := s.refreshes.ListActiveByFamily(ctx, organizationID, familyID)
		if err != nil {
			return err
		}
		for _, t := range tokens {
			t.Revoke()
			if err := s.refreshes.Write(ctx, t); err != nil {
				return err
			}
			if err := s.record(ctx, organizationID, iam.AggregateRefreshToken, t.ID(), t); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *server) Metadata() iam.AuthorizationServerMetadata {
	iss := s.settings.Issuer
	return iam.AuthorizationServerMetadata{
		Issuer:                                     iss,
		AuthorizationEndpoint:                      iss + iam.OAuthAuthorizePath,
		TokenEndpoint:                              iss + iam.OAuthTokenPath,
		RevocationEndpoint:                         iss + iam.OAuthRevokePath,
		JWKSURI:                                    iss + iam.JWKSPath,
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		RevocationEndpointAuthMethodsSupported:     []string{"none"},
		ScopesSupported:                            s.settings.Scopes,
		AuthorizationResponseIssParameterSupported: true,
	}
}

// checkMembership refuses to issue for a person who can no longer act in
// the Organization, such as one suspended since signing in.
func (s *server) checkMembership(ctx context.Context, organizationID, principalID string) error {
	user, err := s.users.Get(ctx, organizationID, principalID)
	if errors.Is(err, iam.ErrNotFound) {
		return invalidGrant("the membership no longer exists")
	}
	if err != nil {
		return err
	}
	if _, err := s.auth.Membership(ctx, user.AccountID(), organizationID); err != nil {
		if errors.Is(err, iam.ErrNoMembership) {
			return invalidGrant("the membership is no longer active")
		}
		return err
	}
	return nil
}

func (s *server) respond(ctx context.Context, client *iam.OAuthClient, rt *iam.OAuthRefreshToken, refresh string) (*iam.TokenResponse, error) {
	access, err := s.issuer.Issue(ctx, token.Subject{
		OrganizationID: rt.OrganizationID(),
		PrincipalID:    rt.PrincipalID(),
		Kind:           identity.ActorUser,
		ClientID:       client.ClientID(),
		Resource:       rt.Resource(),
		Scopes:         rt.Scopes(),
	})
	if err != nil {
		return nil, err
	}
	return &iam.TokenResponse{
		AccessToken:  access.Value,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.settings.AccessTTL / time.Second),
		RefreshToken: refresh,
		Scope:        strings.Join(rt.Scopes(), " "),
	}, nil
}

func (s *server) resource(requested string) (string, error) {
	if requested == "" {
		return s.settings.Resources[0], nil
	}
	if !slices.Contains(s.settings.Resources, requested) {
		return "", fmt.Errorf("%q is not a resource this server issues tokens for", requested)
	}
	return requested, nil
}

// knownScopes keeps the requested scopes the server knows, in a stable order.
func (s *server) knownScopes(scope string) []string {
	var out []string
	for _, sc := range strings.Fields(scope) {
		if slices.Contains(s.settings.Scopes, sc) && !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	slices.Sort(out)
	return out
}

// secret returns 32 random bytes, base64url-encoded.
func (s *server) secret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(s.random, b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// oauthError passes OAuth errors through and logs anything else, which the
// client sees only as server_error.
func (s *server) oauthError(err error) error {
	var oe *iam.OAuthError
	if errors.As(err, &oe) {
		return oe
	}
	s.logger.Error("token request failed", logging.F("error", err))
	return &iam.OAuthError{Code: iam.OAuthServerError}
}

func (s *server) record(ctx context.Context, organizationID, aggregateType, aggregateID string, obj interface{ PullEvents() []aggregate.Event }) error {
	envelopes, err := events.Wrap(ctx, events.Source{OrganizationID: organizationID, AggregateType: aggregateType, AggregateID: aggregateID}, obj.PullEvents()...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", aggregateType, err)
	}
	return s.outbox.Record(ctx, envelopes...)
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func invalidGrant(description string) error {
	return &iam.OAuthError{Code: iam.OAuthInvalidGrant, Description: description}
}

// grantError turns a code's or refresh token's refusal into invalid_grant.
func grantError(err error) error {
	for _, e := range []error{iam.ErrAlreadyUsed, iam.ErrExpired, iam.ErrRevoked, iam.ErrGrantMismatch} {
		if errors.Is(err, e) {
			return invalidGrant(e.Error())
		}
	}
	return err
}

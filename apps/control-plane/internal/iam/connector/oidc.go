package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// GoogleIssuer is Google's OpenID Connect issuer.
const GoogleIssuer = "https://accounts.google.com"

// OIDCConfig configures an OpenID Connect provider.
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	// Name labels the sign-in button.
	Name string
	// Scopes are requested besides openid.
	Scopes []string
	// HTTPClient talks to the provider; nil uses http.DefaultClient.
	HTTPClient *http.Client
}

// GoogleConfig configures Google sign-in.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	// issuer replaces Google's, for tests.
	issuer string
}

type oidcConnector struct {
	protocol iam.IdentityProtocol
	cfg      OIDCConfig

	// The provider's discovery document is fetched on first use, not at
	// start, so a provider that is down does not stop the control plane.
	mu       sync.Mutex
	provider *oidc.Provider
}

// NewOIDC returns a Connector for an OpenID Connect provider.
func NewOIDC(cfg OIDCConfig) (Connector, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		return nil, errors.New("connector: an OIDC provider needs its issuer and client ID")
	}
	if cfg.Name == "" {
		cfg.Name = "Single sign-on"
	}
	return &oidcConnector{protocol: iam.ProtocolOIDC, cfg: cfg}, nil
}

// NewGoogle returns a Connector for Google, which is OpenID Connect with
// Google's issuer. Google says which Workspace an account belongs to in the
// ID token's hd claim.
func NewGoogle(cfg GoogleConfig) (Connector, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("connector: Google needs its client ID")
	}
	issuer := cfg.issuer
	if issuer == "" {
		issuer = GoogleIssuer
	}
	return &oidcConnector{protocol: iam.ProtocolGoogle, cfg: OIDCConfig{
		IssuerURL: issuer, ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		Name: "Google", Scopes: []string{"email", "profile"}, HTTPClient: cfg.HTTPClient,
	}}, nil
}

func (c *oidcConnector) Protocol() iam.IdentityProtocol { return c.protocol }
func (c *oidcConnector) DisplayName() string            { return c.cfg.Name }

func (c *oidcConnector) context(ctx context.Context) context.Context {
	if c.cfg.HTTPClient != nil {
		return oidc.ClientContext(ctx, c.cfg.HTTPClient)
	}
	return ctx
}

func (c *oidcConnector) discover(ctx context.Context) (*oidc.Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.provider, nil
	}
	p, err := oidc.NewProvider(c.context(ctx), c.cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover %s: %w", c.cfg.IssuerURL, err)
	}
	c.provider = p
	return p, nil
}

func (c *oidcConnector) oauth(p *oidc.Provider, redirectURI string) *oauth2.Config {
	scopes := []string{oidc.ScopeOpenID}
	for _, s := range c.cfg.Scopes {
		if !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	return &oauth2.Config{
		ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: redirectURI, Scopes: scopes,
	}
}

func (c *oidcConnector) AuthCodeURL(ctx context.Context, req AuthRequest) (string, error) {
	p, err := c.discover(ctx)
	if err != nil {
		return "", err
	}
	return c.oauth(p, req.RedirectURI).AuthCodeURL(req.State, oidc.Nonce(req.Nonce), oauth2.S256ChallengeOption(req.Verifier)), nil
}

// idClaims are the ID token claims a profile is built from.
type idClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
	HostedDomain  string `json:"hd"`
}

func (c *oidcConnector) Exchange(ctx context.Context, cb Callback) (iam.ExternalProfile, error) {
	p, err := c.discover(ctx)
	if err != nil {
		return iam.ExternalProfile{}, err
	}
	ctx = c.context(ctx)
	tok, err := c.oauth(p, cb.RedirectURI).Exchange(ctx, cb.Code, oauth2.VerifierOption(cb.Verifier))
	if err != nil {
		return iam.ExternalProfile{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return iam.ExternalProfile{}, fmt.Errorf("%w: no ID token", ErrExchange)
	}
	idToken, err := p.Verifier(&oidc.Config{ClientID: c.cfg.ClientID}).Verify(ctx, raw)
	if err != nil {
		return iam.ExternalProfile{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	if idToken.Nonce != cb.Nonce {
		return iam.ExternalProfile{}, fmt.Errorf("%w: the ID token's nonce does not match", ErrExchange)
	}
	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return iam.ExternalProfile{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	return iam.ExternalProfile{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: verified(claims.EmailVerified),
		DisplayName:   claims.Name,
		HostedDomain:  claims.HostedDomain,
	}, nil
}

// verified reads email_verified, which some providers send as a string.
func verified(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	}
	return false
}

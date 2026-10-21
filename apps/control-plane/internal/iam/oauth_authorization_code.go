package iam

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// pkceValue matches a PKCE code verifier or S256 challenge: 43 to 128
// unreserved characters (RFC 7636).
var pkceValue = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// OAuthAuthorizationCode is a short-lived, single-use code that a client
// exchanges for tokens. It binds the request to a PKCE challenge, which must
// be S256, and to the resource the tokens are for (RFC 8707). It is
// identified by the SHA-256 hash of the code; the code itself is not stored.
type OAuthAuthorizationCode struct {
	codeHash       []byte
	organizationID string
	principalID    string
	clientID       string
	redirectURI    string
	codeChallenge  string
	resource       string
	scopes         []string
	expiresAt      time.Time
	usedAt         *time.Time
}

// OAuthAuthorizationCodeSnapshot is an OAuthAuthorizationCode's state, for
// repositories and audit records.
type OAuthAuthorizationCodeSnapshot struct {
	CodeHash       []byte
	OrganizationID string
	PrincipalID    string
	ClientID       string
	RedirectURI    string
	CodeChallenge  string
	Resource       string
	Scopes         []string
	ExpiresAt      time.Time
	UsedAt         *time.Time
}

// NewOAuthAuthorizationCodeProps describes an OAuthAuthorizationCode. A new
// one leaves UsedAt nil; a repository loading a stored one sets it.
type NewOAuthAuthorizationCodeProps struct {
	CodeHash       []byte
	OrganizationID string
	// PrincipalID is who authorized the client.
	PrincipalID string
	// ClientID is the OAuthClient's ID, not its public client_id.
	ClientID    string
	RedirectURI string
	// CodeChallenge is the S256 PKCE challenge.
	CodeChallenge string
	// Resource is the absolute URI of the resource server the tokens are for.
	Resource  string
	Scopes    []string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// NewOAuthAuthorizationCode returns an OAuthAuthorizationCode.
func NewOAuthAuthorizationCode(props NewOAuthAuthorizationCodeProps) (*OAuthAuthorizationCode, error) {
	if err := validateTokenHash("code hash", props.CodeHash); err != nil {
		return nil, err
	}
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	if err := validateID("client ID", OAuthClient{}.Prefix(), props.ClientID); err != nil {
		return nil, err
	}
	if err := validateRequired("redirect URI", props.RedirectURI); err != nil {
		return nil, err
	}
	if !pkceValue.MatchString(props.CodeChallenge) {
		return nil, NewErrInvalidInput("the code challenge must be an S256 PKCE challenge")
	}
	if err := validateResourceIndicator(props.Resource); err != nil {
		return nil, err
	}
	if err := validateScopes(props.Scopes); err != nil {
		return nil, err
	}
	if props.ExpiresAt.IsZero() {
		return nil, NewErrInvalidInput("an expiry time is required")
	}

	return &OAuthAuthorizationCode{
		codeHash:       copyBytes(props.CodeHash),
		organizationID: props.OrganizationID,
		principalID:    props.PrincipalID,
		clientID:       props.ClientID,
		redirectURI:    props.RedirectURI,
		codeChallenge:  props.CodeChallenge,
		resource:       props.Resource,
		scopes:         sortedStrings(props.Scopes),
		expiresAt:      props.ExpiresAt,
		usedAt:         clock.CopyTime(props.UsedAt),
	}, nil
}

// validateResourceIndicator accepts an absolute URI without a fragment (RFC 8707).
func validateResourceIndicator(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || strings.Contains(raw, "#") {
		return NewErrInvalidInput(fmt.Sprintf("resource %q must be an absolute URI without a fragment", raw))
	}
	return nil
}

func validateScopes(scopes []string) error {
	if len(scopes) > maxScopes {
		return NewErrInvalidInput(fmt.Sprintf("at most %d scopes are allowed", maxScopes))
	}
	for i, s := range scopes {
		if s == "" || strings.ContainsAny(s, " \"\\") {
			return NewErrInvalidInput(fmt.Sprintf("scope %q is not a valid OAuth scope", s))
		}
		if slices.Contains(scopes[:i], s) {
			return NewErrInvalidInput(fmt.Sprintf("scope %q appears twice", s))
		}
	}
	return nil
}

func (c *OAuthAuthorizationCode) CodeHash() []byte       { return copyBytes(c.codeHash) }
func (c *OAuthAuthorizationCode) OrganizationID() string { return c.organizationID }
func (c *OAuthAuthorizationCode) PrincipalID() string    { return c.principalID }
func (c *OAuthAuthorizationCode) ClientID() string       { return c.clientID }
func (c *OAuthAuthorizationCode) RedirectURI() string    { return c.redirectURI }
func (c *OAuthAuthorizationCode) CodeChallenge() string  { return c.codeChallenge }
func (c *OAuthAuthorizationCode) Resource() string       { return c.resource }
func (c *OAuthAuthorizationCode) Scopes() []string       { return copyStrings(c.scopes) }
func (c *OAuthAuthorizationCode) ExpiresAt() time.Time   { return c.expiresAt }
func (c *OAuthAuthorizationCode) UsedAt() *time.Time     { return clock.CopyTime(c.usedAt) }

func (c *OAuthAuthorizationCode) Used() bool    { return c.usedAt != nil }
func (c *OAuthAuthorizationCode) Expired() bool { return !clock.Now().Before(c.expiresAt) }

func (c *OAuthAuthorizationCode) Snapshot() OAuthAuthorizationCodeSnapshot {
	return OAuthAuthorizationCodeSnapshot{
		CodeHash:       copyBytes(c.codeHash),
		OrganizationID: c.organizationID,
		PrincipalID:    c.principalID,
		ClientID:       c.clientID,
		RedirectURI:    c.redirectURI,
		CodeChallenge:  c.codeChallenge,
		Resource:       c.resource,
		Scopes:         copyStrings(c.scopes),
		ExpiresAt:      c.expiresAt,
		UsedAt:         clock.CopyTime(c.usedAt),
	}
}

// Redeem exchanges the code, checking that the same client sent the same
// redirect URI and the PKCE verifier for the challenge. A code can be
// redeemed once, before it expires; ErrAlreadyUsed means it was replayed,
// and the tokens issued for it should be revoked.
func (c *OAuthAuthorizationCode) Redeem(clientID, redirectURI, codeVerifier string) error {
	switch {
	case c.Used():
		return fmt.Errorf("%w: authorization code", ErrAlreadyUsed)
	case c.Expired():
		return fmt.Errorf("%w: authorization code", ErrExpired)
	case clientID != c.clientID:
		return fmt.Errorf("%w: issued to another client", ErrGrantMismatch)
	case redirectURI != c.redirectURI:
		return fmt.Errorf("%w: issued for another redirect URI", ErrGrantMismatch)
	case !verifyPKCE(codeVerifier, c.codeChallenge):
		return fmt.Errorf("%w: wrong code verifier", ErrGrantMismatch)
	}
	now := clock.Now()
	c.usedAt = &now
	return nil
}

// verifyPKCE checks verifier against an S256 challenge.
func verifyPKCE(verifier, challenge string) bool {
	if !pkceValue.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

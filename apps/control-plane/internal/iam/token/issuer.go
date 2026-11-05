package token

import (
	"context"
	"errors"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Subject is who an access token is for, and for what.
type Subject struct {
	OrganizationID string
	PrincipalID    string
	Kind           identity.ActorKind
	// ClientID is the OAuth client's public client_id.
	ClientID string
	// Resource is the resource server the token is for, its audience.
	Resource string
	Scopes   []string
}

// Token is an issued access token.
type Token struct {
	Value     string
	ID        string
	ExpiresAt time.Time
}

// Issuer issues access tokens. They are never stored: the resource server
// verifies them offline, so they last only AccessTokenTTL.
type Issuer interface {
	Issue(ctx context.Context, s Subject) (Token, error)
}

type issuer struct {
	keys   KeyRing
	issuer string
	ttl    time.Duration
}

// NewIssuer returns an Issuer signing with keys, as iss, for tokens that
// last ttl.
func NewIssuer(keys KeyRing, iss string, ttl time.Duration) (Issuer, error) {
	switch {
	case keys == nil:
		return nil, errors.New("token: issuer needs keys")
	case iss == "":
		return nil, errors.New("token: issuer needs its URL")
	case ttl <= 0:
		return nil, errors.New("token: token lifetime must be positive")
	}
	return &issuer{keys: keys, issuer: iss, ttl: ttl}, nil
}

func (i *issuer) Issue(_ context.Context, s Subject) (Token, error) {
	now := clock.Now().Truncate(time.Second)
	c := identity.Claims{
		Issuer:         i.issuer,
		Subject:        s.PrincipalID,
		Audience:       []string{s.Resource},
		IssuedAt:       now,
		NotBefore:      now,
		ExpiresAt:      now.Add(i.ttl),
		ID:             id.ULID(),
		OrganizationID: s.OrganizationID,
		ClientID:       s.ClientID,
		Scopes:         s.Scopes,
		Kind:           s.Kind,
	}
	value, err := i.keys.Sign(c)
	if err != nil {
		return Token{}, err
	}
	return Token{Value: value, ID: c.ID, ExpiresAt: c.ExpiresAt}, nil
}

// Package apitokens authenticates requests that carry an API token: a
// long-lived bearer token issued to a service account, such as CI's.
package apitokens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Tokens finds a token by the hash of its secret and records its use.
// iam.APITokenRepository provides it.
type Tokens interface {
	GetByHash(ctx context.Context, hash []byte) (*iam.APIToken, error)
	Write(ctx context.Context, t *iam.APIToken) error
}

// Principals reads the principal a token belongs to.
// iam.PrincipalRepository provides it.
type Principals interface {
	Get(ctx context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error)
}

// ClientID is the client an API token's claims name, so the caller can tell
// an API token's request from an access token's.
const ClientID = "api_token"

// useInterval is how often a token's last use is written: recording every
// request would write to the database on every call CI makes.
const useInterval = time.Minute

// NewVerifier returns an identity.Verifier that accepts API tokens and
// passes every other token to next, which verifies access tokens. An API
// token is accepted while it is not revoked or expired and its owner is an
// active service account or user; it then acts as that principal, with its
// grants, in the token's Organization. Anything else is
// identity.ErrInvalidToken, or identity.ErrTokenExpired for an expired token.
func NewVerifier(tokens Tokens, principals Principals, next identity.Verifier) (identity.Verifier, error) {
	switch {
	case tokens == nil:
		return nil, errors.New("api token verifier: Tokens is required")
	case principals == nil:
		return nil, errors.New("api token verifier: Principals is required")
	case next == nil:
		return nil, errors.New("api token verifier: an access token Verifier is required")
	}
	return &verifier{tokens: tokens, principals: principals, next: next}, nil
}

type verifier struct {
	tokens     Tokens
	principals Principals
	next       identity.Verifier
}

func (v *verifier) Verify(ctx context.Context, token string) (*identity.Claims, error) {
	if !iam.IsAPIToken(token) {
		return v.next.Verify(ctx, token)
	}
	t, err := v.tokens.GetByHash(ctx, iam.HashAPIToken(token))
	if errors.Is(err, iam.ErrNotFound) {
		return nil, fmt.Errorf("%w: unknown api token", identity.ErrInvalidToken)
	}
	if err != nil {
		return nil, fmt.Errorf("find api token: %w", err)
	}
	if err := t.CheckUsable(); err != nil {
		if errors.Is(err, iam.ErrExpired) {
			return nil, fmt.Errorf("%w: %w", identity.ErrTokenExpired, err)
		}
		return nil, fmt.Errorf("%w: %w", identity.ErrInvalidToken, err)
	}
	owner, err := v.principals.Get(ctx, t.OrganizationID(), t.PrincipalID())
	if errors.Is(err, iam.ErrNotFound) {
		return nil, fmt.Errorf("%w: the token's owner no longer exists", identity.ErrInvalidToken)
	}
	if err != nil {
		return nil, fmt.Errorf("find api token owner: %w", err)
	}
	if owner.Status != iam.PrincipalActive {
		return nil, fmt.Errorf("%w: the token's owner is %s", identity.ErrInvalidToken, owner.Status)
	}
	var kind identity.ActorKind
	switch owner.Kind {
	case iam.PrincipalServiceAccount:
		kind = identity.ActorServiceAccount
	case iam.PrincipalUser:
		kind = identity.ActorUser
	default:
		return nil, fmt.Errorf("%w: a %s cannot use api tokens", identity.ErrInvalidToken, owner.Kind)
	}

	if last := t.LastUsedAt(); last == nil || clock.Now().Sub(*last) >= useInterval {
		t.RecordUse()
		// Usage is a convenience for people auditing tokens; failing to
		// record it does not fail the request.
		_ = v.tokens.Write(ctx, t)
	}
	return &identity.Claims{
		Subject:        owner.ID,
		OrganizationID: t.OrganizationID(),
		ClientID:       ClientID,
		Kind:           kind,
		ID:             t.ID(),
		IssuedAt:       t.CreatedAt(),
	}, nil
}

package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// OAuthRefreshToken is a rotating refresh token. Every use replaces it with
// a new token in the same family; if a replaced token is used again, the
// whole family must be revoked, because one of its tokens leaked. Only its
// SHA-256 hash is stored.
type OAuthRefreshToken struct {
	aggregate.Root
	organizationID string
	principalID    string
	clientID       string
	familyID       string
	tokenHash      []byte
	resource       string
	scopes         []string
	expiresAt      time.Time
	replacedBy     *string
	revokedAt      *time.Time
	createdAt      time.Time
}

// OAuthRefreshTokenSnapshot is an OAuthRefreshToken's state, for
// repositories, audit records, and events. Its JSON form is part of the
// OAuthRefreshToken events' contract. Its hash is left out of JSON, so it
// never reaches an event.
type OAuthRefreshTokenSnapshot struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	PrincipalID    string     `json:"principal_id"`
	ClientID       string     `json:"client_id"`
	FamilyID       string     `json:"family_id"`
	TokenHash      []byte     `json:"-"`
	Resource       string     `json:"resource"`
	Scopes         []string   `json:"scopes"`
	ExpiresAt      time.Time  `json:"expires_at"`
	ReplacedBy     *string    `json:"replaced_by"`
	RevokedAt      *time.Time `json:"revoked_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// NewOAuthRefreshTokenProps describes an OAuthRefreshToken. A new one
// leaves FamilyID, ReplacedBy, RevokedAt, and CreatedAt empty; a repository
// loading a stored one sets them.
type NewOAuthRefreshTokenProps struct {
	OrganizationID string
	PrincipalID    string
	// ClientID is the OAuthClient's ID, not its public client_id.
	ClientID string
	// FamilyID is the ID of the first token in the family, and defaults to
	// this token's own ID.
	FamilyID   string
	TokenHash  []byte
	Resource   string
	Scopes     []string
	ExpiresAt  time.Time
	ReplacedBy *string
	RevokedAt  *time.Time
	CreatedAt  *time.Time
}

// NewOAuthRefreshToken returns an OAuthRefreshToken, with a new ID unless
// tokenID gives a stored one.
func NewOAuthRefreshToken(props NewOAuthRefreshTokenProps, tokenID *string) (*OAuthRefreshToken, error) {
	prefix := OAuthRefreshToken{}.Prefix()
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(prefix, tokenID)
	if err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	if err := validateID("client ID", OAuthClient{}.Prefix(), props.ClientID); err != nil {
		return nil, err
	}
	if props.FamilyID != "" {
		if err := validateID("family ID", prefix, props.FamilyID); err != nil {
			return nil, err
		}
	}
	if err := validateTokenHash("token hash", props.TokenHash); err != nil {
		return nil, err
	}
	if err := validateResourceIndicator(props.Resource); err != nil {
		return nil, err
	}
	if err := validateScopes(props.Scopes); err != nil {
		return nil, err
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateExpiry(props.ExpiresAt, createdAt); err != nil {
		return nil, err
	}
	replacedBy := optionalString(props.ReplacedBy)
	if replacedBy != nil {
		if err := validateID("replacement", prefix, *replacedBy); err != nil {
			return nil, err
		}
	}

	t := &OAuthRefreshToken{
		Root:           root,
		organizationID: props.OrganizationID,
		principalID:    props.PrincipalID,
		clientID:       props.ClientID,
		familyID:       props.FamilyID,
		tokenHash:      copyBytes(props.TokenHash),
		resource:       props.Resource,
		scopes:         sortedStrings(props.Scopes),
		expiresAt:      props.ExpiresAt,
		replacedBy:     replacedBy,
		revokedAt:      clock.CopyTime(props.RevokedAt),
		createdAt:      createdAt,
	}
	if t.familyID == "" {
		t.familyID = t.ID()
	}
	if t.IsNew() {
		t.RecordEvent(OAuthRefreshTokenCreated{t.Snapshot()})
	}
	return t, nil
}

func (OAuthRefreshToken) Prefix() string            { return "ort" }
func (t *OAuthRefreshToken) OrganizationID() string { return t.organizationID }
func (t *OAuthRefreshToken) PrincipalID() string    { return t.principalID }
func (t *OAuthRefreshToken) ClientID() string       { return t.clientID }
func (t *OAuthRefreshToken) FamilyID() string       { return t.familyID }
func (t *OAuthRefreshToken) TokenHash() []byte      { return copyBytes(t.tokenHash) }
func (t *OAuthRefreshToken) Resource() string       { return t.resource }
func (t *OAuthRefreshToken) Scopes() []string       { return copyStrings(t.scopes) }
func (t *OAuthRefreshToken) ExpiresAt() time.Time   { return t.expiresAt }
func (t *OAuthRefreshToken) ReplacedBy() *string    { return copyString(t.replacedBy) }
func (t *OAuthRefreshToken) RevokedAt() *time.Time  { return clock.CopyTime(t.revokedAt) }
func (t *OAuthRefreshToken) CreatedAt() time.Time   { return t.createdAt }

func (t *OAuthRefreshToken) Replaced() bool { return t.replacedBy != nil }
func (t *OAuthRefreshToken) Revoked() bool  { return t.revokedAt != nil }
func (t *OAuthRefreshToken) Expired() bool  { return !clock.Now().Before(t.expiresAt) }

func (t *OAuthRefreshToken) Snapshot() OAuthRefreshTokenSnapshot {
	return OAuthRefreshTokenSnapshot{
		ID:             t.ID(),
		OrganizationID: t.organizationID,
		PrincipalID:    t.principalID,
		ClientID:       t.clientID,
		FamilyID:       t.familyID,
		TokenHash:      copyBytes(t.tokenHash),
		Resource:       t.resource,
		Scopes:         copyStrings(t.scopes),
		ExpiresAt:      t.expiresAt,
		ReplacedBy:     copyString(t.replacedBy),
		RevokedAt:      clock.CopyTime(t.revokedAt),
		CreatedAt:      t.createdAt,
	}
}

// Rotate uses the token on behalf of clientID and returns its replacement in
// the same family, for the same principal, resource, and scopes, with the
// new hash and expiry. ErrAlreadyUsed means the token was already replaced:
// it was replayed, and the caller must revoke its family.
func (t *OAuthRefreshToken) Rotate(clientID string, tokenHash []byte, expiresAt time.Time) (*OAuthRefreshToken, error) {
	switch {
	case t.Revoked():
		return nil, fmt.Errorf("%w: refresh token %s", ErrRevoked, t.ID())
	case t.Replaced():
		return nil, fmt.Errorf("%w: refresh token %s was rotated; revoke family %s", ErrAlreadyUsed, t.ID(), t.familyID)
	case t.Expired():
		return nil, fmt.Errorf("%w: refresh token %s", ErrExpired, t.ID())
	case clientID != t.clientID:
		return nil, fmt.Errorf("%w: refresh token %s was issued to another client", ErrGrantMismatch, t.ID())
	}
	next, err := NewOAuthRefreshToken(NewOAuthRefreshTokenProps{
		OrganizationID: t.organizationID,
		PrincipalID:    t.principalID,
		ClientID:       t.clientID,
		FamilyID:       t.familyID,
		TokenHash:      tokenHash,
		Resource:       t.resource,
		Scopes:         t.scopes,
		ExpiresAt:      expiresAt,
	}, nil)
	if err != nil {
		return nil, err
	}
	nextID := next.ID()
	t.replacedBy = &nextID
	t.RecordEvent(OAuthRefreshTokenRotated{t.Snapshot()})
	return next, nil
}

// Revoke ends the token. Revoking a revoked token does nothing.
func (t *OAuthRefreshToken) Revoke() {
	if !t.Revoked() {
		now := clock.Now()
		t.revokedAt = &now
		t.RecordEvent(OAuthRefreshTokenRevoked{t.Snapshot()})
	}
}

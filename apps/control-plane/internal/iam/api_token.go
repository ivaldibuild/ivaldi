package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// APIToken is a long-lived token for a User (CLI, API) or a ServiceAccount.
// It acts with its owner's grants. Only its SHA-256 hash and a short prefix
// for display are stored.
type APIToken struct {
	id             string
	organizationID string
	principalID    string
	name           string
	tokenPrefix    string
	tokenHash      []byte
	expiresAt      *time.Time
	lastUsedAt     *time.Time
	revokedAt      *time.Time
	createdAt      time.Time
}

// APITokenSnapshot is an APIToken's state, for repositories and audit records.
type APITokenSnapshot struct {
	ID             string
	OrganizationID string
	PrincipalID    string
	Name           string
	TokenPrefix    string
	TokenHash      []byte
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// NewAPITokenProps describes an APIToken. A new one leaves LastUsedAt,
// RevokedAt, and CreatedAt nil; a repository loading a stored one sets them.
type NewAPITokenProps struct {
	OrganizationID string
	// PrincipalID is the owning User or ServiceAccount.
	PrincipalID string
	Name        string
	// TokenPrefix is the start of the token, shown so people can tell their
	// tokens apart.
	TokenPrefix string
	// TokenHash is the SHA-256 hash of the whole token.
	TokenHash []byte
	// ExpiresAt is nil for a token that does not expire.
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  *time.Time
}

// NewAPIToken returns an APIToken, with a new ID unless apiTokenID gives a stored one.
func NewAPIToken(props NewAPITokenProps, apiTokenID *string) (*APIToken, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateOwnID(APIToken{}.Prefix(), apiTokenID); err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}
	if props.TokenPrefix == "" || len(props.TokenPrefix) > maxTokenPrefixLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("token prefix must be 1-%d bytes", maxTokenPrefixLength))
	}
	if err := validateTokenHash("token hash", props.TokenHash); err != nil {
		return nil, err
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateOptionalExpiry(props.ExpiresAt, createdAt); err != nil {
		return nil, err
	}

	t := &APIToken{
		organizationID: props.OrganizationID,
		principalID:    props.PrincipalID,
		name:           props.Name,
		tokenPrefix:    props.TokenPrefix,
		tokenHash:      copyBytes(props.TokenHash),
		expiresAt:      clock.CopyTime(props.ExpiresAt),
		lastUsedAt:     clock.CopyTime(props.LastUsedAt),
		revokedAt:      clock.CopyTime(props.RevokedAt),
		createdAt:      createdAt,
	}
	if apiTokenID != nil {
		t.id = *apiTokenID
	}
	id.Assign(&t.id, t)
	return t, nil
}

func (APIToken) Prefix() string            { return "tok" }
func (t *APIToken) ID() string             { return t.id }
func (t *APIToken) OrganizationID() string { return t.organizationID }
func (t *APIToken) PrincipalID() string    { return t.principalID }
func (t *APIToken) Name() string           { return t.name }
func (t *APIToken) TokenPrefix() string    { return t.tokenPrefix }
func (t *APIToken) TokenHash() []byte      { return copyBytes(t.tokenHash) }
func (t *APIToken) ExpiresAt() *time.Time  { return clock.CopyTime(t.expiresAt) }
func (t *APIToken) LastUsedAt() *time.Time { return clock.CopyTime(t.lastUsedAt) }
func (t *APIToken) RevokedAt() *time.Time  { return clock.CopyTime(t.revokedAt) }
func (t *APIToken) CreatedAt() time.Time   { return t.createdAt }

func (t *APIToken) Revoked() bool { return t.revokedAt != nil }
func (t *APIToken) Expired() bool { return t.expiresAt != nil && !clock.Now().Before(*t.expiresAt) }

// CheckUsable returns an error if the token is revoked or expired. Its
// owner must be checked too.
func (t *APIToken) CheckUsable() error {
	switch {
	case t.Revoked():
		return fmt.Errorf("%w: api token %s", ErrRevoked, t.id)
	case t.Expired():
		return fmt.Errorf("%w: api token %s", ErrExpired, t.id)
	}
	return nil
}

func (t *APIToken) Snapshot() APITokenSnapshot {
	return APITokenSnapshot{
		ID:             t.id,
		OrganizationID: t.organizationID,
		PrincipalID:    t.principalID,
		Name:           t.name,
		TokenPrefix:    t.tokenPrefix,
		TokenHash:      copyBytes(t.tokenHash),
		ExpiresAt:      clock.CopyTime(t.expiresAt),
		LastUsedAt:     clock.CopyTime(t.lastUsedAt),
		RevokedAt:      clock.CopyTime(t.revokedAt),
		CreatedAt:      t.createdAt,
	}
}

// RecordUse records that the token authenticated a request now.
func (t *APIToken) RecordUse() {
	now := clock.Now()
	t.lastUsedAt = &now
}

// Revoke ends the token for good. Revoking a revoked token does nothing.
func (t *APIToken) Revoke() {
	if !t.Revoked() {
		now := clock.Now()
		t.revokedAt = &now
	}
}

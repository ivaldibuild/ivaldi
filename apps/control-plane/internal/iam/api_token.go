package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// APIToken is a long-lived token for a User (CLI, API) or a ServiceAccount.
// It acts with its owner's grants. Only its SHA-256 hash and a short prefix
// for display are stored.
type APIToken struct {
	aggregate.Root
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

// APITokenSnapshot is an APIToken's state, for repositories, audit records,
// and events. Its JSON form is part of the APIToken events' contract. Its
// hash is left out of JSON, so it never reaches an event.
type APITokenSnapshot struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	PrincipalID    string     `json:"principal_id"`
	Name           string     `json:"name"`
	TokenPrefix    string     `json:"token_prefix"`
	TokenHash      []byte     `json:"-"`
	ExpiresAt      *time.Time `json:"expires_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	RevokedAt      *time.Time `json:"revoked_at"`
	CreatedAt      time.Time  `json:"created_at"`
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
	root, err := newRoot(APIToken{}.Prefix(), apiTokenID)
	if err != nil {
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
		Root:           root,
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
	if t.IsNew() {
		t.RecordEvent(APITokenCreated{t.Snapshot()})
	}
	return t, nil
}

func (APIToken) Prefix() string            { return "tok" }
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
		return fmt.Errorf("%w: api token %s", ErrRevoked, t.ID())
	case t.Expired():
		return fmt.Errorf("%w: api token %s", ErrExpired, t.ID())
	}
	return nil
}

func (t *APIToken) Snapshot() APITokenSnapshot {
	return APITokenSnapshot{
		ID:             t.ID(),
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
		t.RecordEvent(APITokenRevoked{t.Snapshot()})
	}
}

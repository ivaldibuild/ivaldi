package iam

import (
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// argon2idPrefix starts every PHC-format argon2id hash.
const argon2idPrefix = "$argon2id$"

// PasswordCredential is an Account's password for the builtin identity
// provider. It is platform-scoped, so a person has one password across all
// their Organizations, and is identified by its Account. Only an argon2id
// hash is stored; hashing and checking passwords happen outside the domain.
type PasswordCredential struct {
	aggregate.Events
	accountID    string
	passwordHash string
	updatedAt    time.Time
}

// PasswordCredentialSnapshot is a PasswordCredential's state, for
// repositories, audit records, and events. Its JSON form is part of the
// PasswordCredential events' contract. Its hash is left out of JSON, so it
// never reaches an event.
type PasswordCredentialSnapshot struct {
	AccountID    string    `json:"account_id"`
	PasswordHash string    `json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewPasswordCredentialProps describes a PasswordCredential. A new one
// leaves UpdatedAt nil; a repository loading a stored one sets it.
type NewPasswordCredentialProps struct {
	AccountID string
	// PasswordHash is an argon2id hash in PHC format, such as
	// "$argon2id$v=19$m=65536,t=3,p=4$…".
	PasswordHash string
	UpdatedAt    *time.Time
}

// NewPasswordCredential returns a PasswordCredential.
func NewPasswordCredential(props NewPasswordCredentialProps) (*PasswordCredential, error) {
	if err := validateID("account ID", Account{}.Prefix(), props.AccountID); err != nil {
		return nil, err
	}
	if err := validatePasswordHash(props.PasswordHash); err != nil {
		return nil, err
	}
	c := &PasswordCredential{
		accountID:    props.AccountID,
		passwordHash: props.PasswordHash,
		updatedAt:    timeOr(props.UpdatedAt, clock.Now()),
	}
	// A PasswordCredential has no ID to tell a new one from a stored one; a
	// stored one has the time it last changed.
	if props.UpdatedAt == nil {
		c.RecordEvent(PasswordCredentialCreated{c.Snapshot()})
	}
	return c, nil
}

// validatePasswordHash refuses anything but an argon2id hash, so a raw
// password is never stored by mistake.
func validatePasswordHash(hash string) error {
	if !strings.HasPrefix(hash, argon2idPrefix) || len(hash) <= len(argon2idPrefix) {
		return NewErrInvalidInput("the password hash must be an argon2id hash in PHC format")
	}
	return nil
}

func (c *PasswordCredential) AccountID() string    { return c.accountID }
func (c *PasswordCredential) PasswordHash() string { return c.passwordHash }
func (c *PasswordCredential) UpdatedAt() time.Time { return c.updatedAt }

func (c *PasswordCredential) Snapshot() PasswordCredentialSnapshot {
	return PasswordCredentialSnapshot{
		AccountID:    c.accountID,
		PasswordHash: c.passwordHash,
		UpdatedAt:    c.updatedAt,
	}
}

// Change replaces the password hash.
func (c *PasswordCredential) Change(passwordHash string) error {
	if err := validatePasswordHash(passwordHash); err != nil {
		return err
	}
	changed := passwordHash != c.passwordHash
	c.passwordHash = passwordHash
	c.updatedAt = clock.Now()
	if changed {
		c.RecordEvent(PasswordCredentialChanged{c.Snapshot()})
	}
	return nil
}

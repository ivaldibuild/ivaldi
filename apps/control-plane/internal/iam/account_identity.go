package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// PlatformProvider is a provider the managed cloud signs people in with
// before they belong to any Organization.
type PlatformProvider string

const (
	PlatformGoogle PlatformProvider = "google"
	PlatformGitHub PlatformProvider = "github"
)

// AccountIdentity links an Account to the subject a platform provider
// asserts for it, so a person can sign up for and sign in to the managed
// cloud with an existing Google or GitHub account. Unlike an
// ExternalIdentity, which an Organization's identity provider links to one
// User, it authenticates the Account, and the person then chooses one of
// their Organizations. Each subject links to one Account per provider.
type AccountIdentity struct {
	aggregate.Root
	accountID   string
	provider    PlatformProvider
	subject     string
	lastLoginAt *time.Time
	createdAt   time.Time
}

// AccountIdentitySnapshot is an AccountIdentity's state, for repositories,
// audit records, and events. Its JSON form is part of the AccountIdentity
// events' contract.
type AccountIdentitySnapshot struct {
	ID          string           `json:"id"`
	AccountID   string           `json:"account_id"`
	Provider    PlatformProvider `json:"provider"`
	Subject     string           `json:"subject"`
	LastLoginAt *time.Time       `json:"last_login_at"`
	CreatedAt   time.Time        `json:"created_at"`
}

// NewAccountIdentityProps describes an AccountIdentity. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewAccountIdentityProps struct {
	AccountID   string
	Provider    PlatformProvider
	Subject     string
	LastLoginAt *time.Time
	CreatedAt   *time.Time
}

// NewAccountIdentity returns an AccountIdentity, with a new ID unless
// accountIdentityID gives a stored one.
func NewAccountIdentity(props NewAccountIdentityProps, accountIdentityID *string) (*AccountIdentity, error) {
	root, err := newRoot(AccountIdentity{}.Prefix(), accountIdentityID)
	if err != nil {
		return nil, err
	}
	if err := validateID("account ID", Account{}.Prefix(), props.AccountID); err != nil {
		return nil, err
	}
	switch props.Provider {
	case PlatformGoogle, PlatformGitHub:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("platform provider %q must be google or github", props.Provider))
	}
	if props.Subject == "" || len(props.Subject) > maxSubjectLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("subject must be 1-%d bytes", maxSubjectLength))
	}

	a := &AccountIdentity{
		Root:        root,
		accountID:   props.AccountID,
		provider:    props.Provider,
		subject:     props.Subject,
		lastLoginAt: clock.CopyTime(props.LastLoginAt),
		createdAt:   timeOr(props.CreatedAt, clock.Now()),
	}
	if a.IsNew() {
		a.RecordEvent(AccountIdentityCreated{a.Snapshot()})
	}
	return a, nil
}

func (AccountIdentity) Prefix() string                { return "aid" }
func (a *AccountIdentity) AccountID() string          { return a.accountID }
func (a *AccountIdentity) Provider() PlatformProvider { return a.provider }
func (a *AccountIdentity) Subject() string            { return a.subject }
func (a *AccountIdentity) LastLoginAt() *time.Time    { return clock.CopyTime(a.lastLoginAt) }
func (a *AccountIdentity) CreatedAt() time.Time       { return a.createdAt }

func (a *AccountIdentity) Snapshot() AccountIdentitySnapshot {
	return AccountIdentitySnapshot{
		ID:          a.ID(),
		AccountID:   a.accountID,
		Provider:    a.provider,
		Subject:     a.subject,
		LastLoginAt: clock.CopyTime(a.lastLoginAt),
		CreatedAt:   a.createdAt,
	}
}

// RecordLogin records that the identity signed in now.
func (a *AccountIdentity) RecordLogin() {
	now := clock.Now()
	a.lastLoginAt = &now
}

package iam

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// AccountStatus is where an account is in its life. Accounts are never
// deleted, because audit records refer to their memberships.
type AccountStatus string

const (
	AccountActive      AccountStatus = "active"
	AccountSuspended   AccountStatus = "suspended"
	AccountDeactivated AccountStatus = "deactivated"
)

// Account is a person, across the platform. It has no access on its own:
// access comes through its memberships, one User per Organization. It is
// platform-scoped, so it has no Organization.
type Account struct {
	aggregate.Root
	email           string
	emailVerifiedAt *time.Time
	displayName     string
	status          AccountStatus
	createdAt       time.Time
	updatedAt       time.Time
}

// AccountSnapshot is an Account's state, for repositories and audit records.
type AccountSnapshot struct {
	ID              string
	Email           string
	EmailVerifiedAt *time.Time
	DisplayName     string
	Status          AccountStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewAccountProps describes an Account. A new one leaves Status and the
// timestamps empty; a repository loading a stored one sets them.
type NewAccountProps struct {
	// Email is unique across the platform, ignoring case.
	Email           string
	EmailVerifiedAt *time.Time
	DisplayName     string
	// Status defaults to AccountActive.
	Status    AccountStatus
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// NewAccount returns an Account, with a new ID unless accountID gives a stored one.
func NewAccount(props NewAccountProps, accountID *string) (*Account, error) {
	root, err := newRoot(Account{}.Prefix(), accountID)
	if err != nil {
		return nil, err
	}
	if err := validateEmail(props.Email); err != nil {
		return nil, err
	}
	if err := validateName("display name", props.DisplayName, maxNameLength); err != nil {
		return nil, err
	}
	if props.Status == "" {
		props.Status = AccountActive
	}
	if err := validateAccountStatus(props.Status); err != nil {
		return nil, err
	}

	now := clock.Now()
	a := &Account{
		Root:            root,
		email:           props.Email,
		emailVerifiedAt: clock.CopyTime(props.EmailVerifiedAt),
		displayName:     props.DisplayName,
		status:          props.Status,
		createdAt:       timeOr(props.CreatedAt, now),
		updatedAt:       timeOr(props.UpdatedAt, now),
	}
	return a, nil
}

// validateEmail accepts a bare address such as ada@example.com, without a
// display name or angle brackets.
func validateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" || len(email) > maxEmailLength {
		return NewErrInvalidInput(fmt.Sprintf("%q is not an email address", email))
	}
	return nil
}

func validateAccountStatus(s AccountStatus) error {
	switch s {
	case AccountActive, AccountSuspended, AccountDeactivated:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("account status %q must be active, suspended, or deactivated", s))
}

func (Account) Prefix() string                 { return "acc" }
func (a *Account) Email() string               { return a.email }
func (a *Account) EmailVerifiedAt() *time.Time { return clock.CopyTime(a.emailVerifiedAt) }
func (a *Account) DisplayName() string         { return a.displayName }
func (a *Account) Status() AccountStatus       { return a.status }
func (a *Account) CreatedAt() time.Time        { return a.createdAt }
func (a *Account) UpdatedAt() time.Time        { return a.updatedAt }

func (a *Account) Active() bool        { return a.status == AccountActive }
func (a *Account) EmailVerified() bool { return a.emailVerifiedAt != nil }

// EmailDomain returns the domain of the account's email, in lowercase.
func (a *Account) EmailDomain() string {
	return strings.ToLower(a.email[strings.LastIndexByte(a.email, '@')+1:])
}

func (a *Account) Snapshot() AccountSnapshot {
	return AccountSnapshot{
		ID:              a.ID(),
		Email:           a.email,
		EmailVerifiedAt: clock.CopyTime(a.emailVerifiedAt),
		DisplayName:     a.displayName,
		Status:          a.status,
		CreatedAt:       a.createdAt,
		UpdatedAt:       a.updatedAt,
	}
}

// Rename changes the account's default display name.
func (a *Account) Rename(displayName string) error {
	if err := validateName("display name", displayName, maxNameLength); err != nil {
		return err
	}
	a.displayName = displayName
	a.updatedAt = clock.Now()
	return nil
}

// ChangeEmail changes the sign-in email, which must then be verified again.
// Changing only its case keeps the verification.
func (a *Account) ChangeEmail(email string) error {
	if err := validateEmail(email); err != nil {
		return err
	}
	if !strings.EqualFold(email, a.email) {
		a.emailVerifiedAt = nil
	}
	a.email = email
	a.updatedAt = clock.Now()
	return nil
}

// VerifyEmail records that the account holder proved they own the email.
// Verifying a verified email does nothing.
func (a *Account) VerifyEmail() {
	if !a.EmailVerified() {
		now := clock.Now()
		a.emailVerifiedAt = &now
		a.updatedAt = now
	}
}

// Suspend stops the account from signing in. It cannot suspend a
// deactivated account, and suspending a suspended one does nothing.
func (a *Account) Suspend() error {
	return a.setStatus(AccountSuspended, AccountActive)
}

// Deactivate closes the account. Deactivating a deactivated one does nothing.
func (a *Account) Deactivate() error {
	return a.setStatus(AccountDeactivated, AccountActive, AccountSuspended)
}

// Reactivate makes a suspended or deactivated account active again.
func (a *Account) Reactivate() error {
	return a.setStatus(AccountActive, AccountSuspended, AccountDeactivated)
}

// setStatus moves the account to status from one of the from statuses.
func (a *Account) setStatus(status AccountStatus, from ...AccountStatus) error {
	if a.status == status {
		return nil
	}
	for _, f := range from {
		if a.status == f {
			a.status = status
			a.updatedAt = clock.Now()
			return nil
		}
	}
	return fmt.Errorf("%w: account %s is %s", ErrStatus, a.ID(), a.status)
}

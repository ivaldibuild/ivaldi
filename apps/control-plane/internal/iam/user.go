package iam

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// User is one Account's membership in one Organization. Grants, group
// memberships, API tokens, and external identities point to the User, never
// to the Account, so removing someone from an Organization deactivates
// their User and leaves their Account and other memberships alone.
type User struct {
	principal
	accountID string
	joinedAt  time.Time
}

// UserSnapshot is a User's state, for repositories and audit records.
type UserSnapshot struct {
	PrincipalSnapshot
	AccountID string
	JoinedAt  time.Time
}

// NewUserProps describes a User. A new one leaves JoinedAt nil; a
// repository loading a stored one sets it.
type NewUserProps struct {
	PrincipalProps
	AccountID string
	JoinedAt  *time.Time
}

// NewUser returns a User, with a new ID unless principalID gives a stored one.
func NewUser(props NewUserProps, principalID *string) (*User, error) {
	p, err := newPrincipal(PrincipalUser, props.PrincipalProps, principalID)
	if err != nil {
		return nil, err
	}
	if err := validateID("account ID", Account{}.Prefix(), props.AccountID); err != nil {
		return nil, err
	}
	return &User{
		principal: p,
		accountID: props.AccountID,
		joinedAt:  timeOr(props.JoinedAt, clock.Now()),
	}, nil
}

func (u *User) AccountID() string   { return u.accountID }
func (u *User) JoinedAt() time.Time { return u.joinedAt }

func (u *User) Snapshot() UserSnapshot {
	return UserSnapshot{
		PrincipalSnapshot: u.snapshot(),
		AccountID:         u.accountID,
		JoinedAt:          u.joinedAt,
	}
}

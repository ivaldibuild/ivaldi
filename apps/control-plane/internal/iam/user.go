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
	// lastSignedInAt is usage tracking: setting it records no event.
	lastSignedInAt *time.Time
}

// UserSnapshot is a User's state, for repositories, audit records, and
// events. Its JSON form is part of the User events' contract.
type UserSnapshot struct {
	PrincipalSnapshot
	AccountID      string     `json:"account_id"`
	JoinedAt       time.Time  `json:"joined_at"`
	LastSignedInAt *time.Time `json:"last_signed_in_at"`
}

// NewUserProps describes a User. A new one leaves JoinedAt and
// LastSignedInAt nil; a repository loading a stored one sets them.
type NewUserProps struct {
	PrincipalProps
	AccountID      string
	JoinedAt       *time.Time
	LastSignedInAt *time.Time
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
	u := &User{
		principal: p,
		accountID: props.AccountID,
		joinedAt:  timeOr(props.JoinedAt, clock.Now()),

		lastSignedInAt: clock.CopyTime(props.LastSignedInAt),
	}
	if u.IsNew() {
		u.RecordEvent(UserCreated{u.Snapshot()})
	}
	return u, nil
}

func (u *User) AccountID() string   { return u.accountID }
func (u *User) JoinedAt() time.Time { return u.joinedAt }

// LastSignedInAt is when the user last signed in to the Organization, or
// nil before they first do.
func (u *User) LastSignedInAt() *time.Time { return clock.CopyTime(u.lastSignedInAt) }

// RecordSignIn records that the user signed in to the Organization now. It
// is usage tracking, so it records no event.
func (u *User) RecordSignIn() {
	now := clock.Now()
	u.lastSignedInAt = &now
}

func (u *User) Snapshot() UserSnapshot {
	return UserSnapshot{
		PrincipalSnapshot: u.snapshot(),
		AccountID:         u.accountID,
		JoinedAt:          u.joinedAt,
		LastSignedInAt:    clock.CopyTime(u.lastSignedInAt),
	}
}

package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func accountProps() NewAccountProps {
	return NewAccountProps{
		Email:       "Ada@Acme.Example",
		DisplayName: "Ada Lovelace",
	}
}

func accountNew(t *testing.T) *Account {
	t.Helper()
	a, err := NewAccount(accountProps(), nil)
	require.NoError(t, err)
	return a
}

func TestNewAccount(t *testing.T) {
	clock.Fix(t, t0)

	a, err := NewAccount(accountProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("acc", a.ID()), "a new account gets an acc_ ID: %q", a.ID())
	assert.Equal(t, "Ada@Acme.Example", a.Email(), "the email keeps its case")
	assert.Equal(t, "acme.example", a.EmailDomain())
	assert.Nil(t, a.EmailVerifiedAt())
	assert.False(t, a.EmailVerified())
	assert.Equal(t, "Ada Lovelace", a.DisplayName())
	assert.Equal(t, AccountActive, a.Status(), "a new account is active")
	assert.True(t, a.Active())
	assert.Equal(t, t0, a.CreatedAt())
	assert.Equal(t, t0, a.UpdatedAt())
}

func TestNewAccountWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("acc")
	props := accountProps()
	props.EmailVerifiedAt = new(t0)
	props.Status = AccountSuspended
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)

	a, err := NewAccount(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, a.ID())
	assert.True(t, a.EmailVerified())
	assert.Equal(t, t0, *a.EmailVerifiedAt())
	assert.Equal(t, AccountSuspended, a.Status())
	assert.False(t, a.Active())
	assert.Equal(t, t0, a.CreatedAt())
	assert.Equal(t, t1, a.UpdatedAt())

	*props.EmailVerifiedAt = t2
	assert.Equal(t, t0, *a.EmailVerifiedAt(), "the given time is copied")
}

func TestNewAccountIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewAccountProps)
		id     *string
	}{
		{"no email", func(p *NewAccountProps) { p.Email = "" }, nil},
		{"not an email", func(p *NewAccountProps) { p.Email = "ada" }, nil},
		{"email with display name", func(p *NewAccountProps) { p.Email = "Ada <ada@acme.example>" }, nil},
		{"email in angle brackets", func(p *NewAccountProps) { p.Email = "<ada@acme.example>" }, nil},
		{"email with spaces", func(p *NewAccountProps) { p.Email = " ada@acme.example" }, nil},
		{"email too long", func(p *NewAccountProps) {
			p.Email = strings.Repeat("a", 64) + "@" + strings.Repeat("b", 260) + ".example"
		}, nil},
		{"no display name", func(p *NewAccountProps) { p.DisplayName = "" }, nil},
		{"display name too long", func(p *NewAccountProps) { p.DisplayName = strings.Repeat("é", maxNameLength+1) }, nil},
		{"unknown status", func(p *NewAccountProps) { p.Status = "deleted" }, nil},
		{"ID with wrong prefix", func(*NewAccountProps) {}, new(id.New("org"))},
		{"malformed ID", func(*NewAccountProps) {}, new("acc_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := accountProps()
			tt.mutate(&props)
			_, err := NewAccount(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAccountGettersReturnCopies(t *testing.T) {
	props := accountProps()
	props.EmailVerifiedAt = new(t0)
	a, err := NewAccount(props, nil)
	require.NoError(t, err)

	*a.EmailVerifiedAt() = t2
	snap := a.Snapshot()
	*snap.EmailVerifiedAt = t2

	assert.Equal(t, t0, *a.EmailVerifiedAt())
}

func TestAccountSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	a := accountNew(t)

	assert.Equal(t, AccountSnapshot{
		ID:          a.ID(),
		Email:       "Ada@Acme.Example",
		DisplayName: "Ada Lovelace",
		Status:      AccountActive,
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}, a.Snapshot())
}

func TestAccountRename(t *testing.T) {
	clock.Fix(t, t0)
	a := accountNew(t)
	clock.Fix(t, t1)

	require.NoError(t, a.Rename("Ada King"))
	assert.Equal(t, "Ada King", a.DisplayName())
	assert.Equal(t, t1, a.UpdatedAt())

	assert.ErrorIs(t, a.Rename(""), ErrInvalid)
	assert.Equal(t, "Ada King", a.DisplayName())
}

func TestAccountChangeEmail(t *testing.T) {
	clock.Fix(t, t0)
	a := accountNew(t)
	a.VerifyEmail()

	clock.Fix(t, t1)
	require.NoError(t, a.ChangeEmail("ada@acme.example"))
	assert.Equal(t, "ada@acme.example", a.Email())
	assert.True(t, a.EmailVerified(), "changing only the case keeps the verification")
	assert.Equal(t, t1, a.UpdatedAt())

	require.NoError(t, a.ChangeEmail("ada@other.example"))
	assert.Equal(t, "ada@other.example", a.Email())
	assert.False(t, a.EmailVerified(), "a new email must be verified again")

	assert.ErrorIs(t, a.ChangeEmail("not-an-email"), ErrInvalid)
	assert.Equal(t, "ada@other.example", a.Email())
}

func TestAccountVerifyEmail(t *testing.T) {
	clock.Fix(t, t0)
	a := accountNew(t)
	clock.Fix(t, t1)

	a.VerifyEmail()

	require.True(t, a.EmailVerified())
	assert.Equal(t, t1, *a.EmailVerifiedAt())
	assert.Equal(t, t1, a.UpdatedAt())

	clock.Fix(t, t2)
	a.VerifyEmail()
	assert.Equal(t, t1, *a.EmailVerifiedAt(), "verifying a verified email does nothing")
	assert.Equal(t, t1, a.UpdatedAt())
}

func TestAccountStatusChanges(t *testing.T) {
	tests := []struct {
		name    string
		from    AccountStatus
		change  func(*Account) error
		want    AccountStatus
		wantErr error
	}{
		{"suspend active", AccountActive, (*Account).Suspend, AccountSuspended, nil},
		{"suspend suspended", AccountSuspended, (*Account).Suspend, AccountSuspended, nil},
		{"suspend deactivated", AccountDeactivated, (*Account).Suspend, AccountDeactivated, ErrStatus},
		{"deactivate active", AccountActive, (*Account).Deactivate, AccountDeactivated, nil},
		{"deactivate suspended", AccountSuspended, (*Account).Deactivate, AccountDeactivated, nil},
		{"deactivate deactivated", AccountDeactivated, (*Account).Deactivate, AccountDeactivated, nil},
		{"reactivate suspended", AccountSuspended, (*Account).Reactivate, AccountActive, nil},
		{"reactivate deactivated", AccountDeactivated, (*Account).Reactivate, AccountActive, nil},
		{"reactivate active", AccountActive, (*Account).Reactivate, AccountActive, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := accountProps()
			props.Status = tt.from
			a, err := NewAccount(props, nil)
			require.NoError(t, err)
			clock.Fix(t, t1)

			err = tt.change(a)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, a.Status())
			if tt.from == tt.want {
				assert.Equal(t, t0, a.UpdatedAt(), "no change leaves UpdatedAt alone")
			} else {
				assert.Equal(t, t1, a.UpdatedAt())
			}
		})
	}
}

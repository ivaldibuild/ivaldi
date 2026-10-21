package iam

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

const (
	pwHash      = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo"
	pwOtherHash = "$argon2id$v=19$m=65536,t=3,p=4$b3RoZXJzYWx0$b3RoZXJoYXNo"
)

func TestNewPasswordCredential(t *testing.T) {
	clock.Fix(t, t0)

	c, err := NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash})

	require.NoError(t, err)
	assert.Equal(t, testAccount, c.AccountID())
	assert.Equal(t, pwHash, c.PasswordHash())
	assert.Equal(t, t0, c.UpdatedAt())
}

func TestNewPasswordCredentialWithStoredState(t *testing.T) {
	clock.Fix(t, t2)

	c, err := NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash, UpdatedAt: new(t0)})

	require.NoError(t, err)
	assert.Equal(t, t0, c.UpdatedAt(), "a given timestamp is kept")
}

func TestNewPasswordCredentialIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		props NewPasswordCredentialProps
	}{
		{"no account", NewPasswordCredentialProps{PasswordHash: pwHash}},
		{"principal as account", NewPasswordCredentialProps{AccountID: testPrincipal, PasswordHash: pwHash}},
		{"no hash", NewPasswordCredentialProps{AccountID: testAccount}},
		{"raw password", NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: "correct horse battery staple"}},
		{"bcrypt hash", NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: "$2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW"}},
		{"argon2i hash", NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"}},
		{"prefix only", NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: "$argon2id$"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPasswordCredential(tt.props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestPasswordCredentialChange(t *testing.T) {
	clock.Fix(t, t0)
	c, err := NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash})
	require.NoError(t, err)
	clock.Fix(t, t1)

	require.NoError(t, c.Change(pwOtherHash))

	assert.Equal(t, pwOtherHash, c.PasswordHash())
	assert.Equal(t, t1, c.UpdatedAt())
}

func TestPasswordCredentialChangeIsRefused(t *testing.T) {
	clock.Fix(t, t0)
	c, err := NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash})
	require.NoError(t, err)
	clock.Fix(t, t1)

	assert.ErrorIs(t, c.Change("hunter2"), ErrInvalid)

	assert.Equal(t, pwHash, c.PasswordHash(), "nothing changes")
	assert.Equal(t, t0, c.UpdatedAt())
}

func TestPasswordCredentialSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	c, err := NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash})
	require.NoError(t, err)

	assert.Equal(t, PasswordCredentialSnapshot{
		AccountID:    testAccount,
		PasswordHash: pwHash,
		UpdatedAt:    t0,
	}, c.Snapshot())
}

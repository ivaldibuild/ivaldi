package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func aidProps() NewAccountIdentityProps {
	return NewAccountIdentityProps{AccountID: testAccount, Provider: PlatformGitHub, Subject: "583231"}
}

func TestNewAccountIdentity(t *testing.T) {
	clock.Fix(t, t0)

	a, err := NewAccountIdentity(aidProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("aid", a.ID()), a.ID())
	assert.Equal(t, testAccount, a.AccountID())
	assert.Equal(t, PlatformGitHub, a.Provider())
	assert.Equal(t, "583231", a.Subject())
	assert.Equal(t, t0, a.CreatedAt())
	assert.Nil(t, a.LastLoginAt())
}

func TestNewAccountIdentityIsRefused(t *testing.T) {
	tests := map[string]func(*NewAccountIdentityProps){
		"no account":       func(p *NewAccountIdentityProps) { p.AccountID = "" },
		"not an account":   func(p *NewAccountIdentityProps) { p.AccountID = testOrg },
		"unknown provider": func(p *NewAccountIdentityProps) { p.Provider = "gitlab" },
		"no subject":       func(p *NewAccountIdentityProps) { p.Subject = "" },
		"subject too long": func(p *NewAccountIdentityProps) { p.Subject = strings.Repeat("x", maxSubjectLength+1) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			props := aidProps()
			mutate(&props)
			_, err := NewAccountIdentity(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAccountIdentityRecordLogin(t *testing.T) {
	a := mustBuild(NewAccountIdentity(aidProps(), nil))
	clock.Fix(t, t0)

	a.RecordLogin()

	require.NotNil(t, a.LastLoginAt())
	assert.Equal(t, t0, *a.LastLoginAt())
}

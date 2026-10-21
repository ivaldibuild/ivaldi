package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func xidProps() NewExternalIdentityProps {
	return NewExternalIdentityProps{
		OrganizationID:     testOrg,
		PrincipalID:        testPrincipal,
		IdentityProviderID: testIdP,
		Subject:            "248289761001",
	}
}

func TestNewExternalIdentity(t *testing.T) {
	clock.Fix(t, t0)

	e, err := NewExternalIdentity(xidProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("xid", e.ID()), "a new external identity gets an xid_ ID: %q", e.ID())
	assert.Equal(t, testOrg, e.OrganizationID())
	assert.Equal(t, testPrincipal, e.PrincipalID())
	assert.Equal(t, testIdP, e.IdentityProviderID())
	assert.Equal(t, "248289761001", e.Subject())
	assert.Nil(t, e.LastLoginAt())
	assert.Equal(t, t0, e.CreatedAt())
}

func TestNewExternalIdentityWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("xid")
	props := xidProps()
	props.CreatedAt = new(t0)
	props.LastLoginAt = new(t1)

	e, err := NewExternalIdentity(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, e.ID())
	assert.Equal(t, t0, e.CreatedAt())
	require.NotNil(t, e.LastLoginAt())
	assert.Equal(t, t1, *e.LastLoginAt())

	*props.LastLoginAt = t2
	assert.Equal(t, t1, *e.LastLoginAt(), "the given time is copied")
}

func TestNewExternalIdentityIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewExternalIdentityProps)
		id     *string
	}{
		{"no organization", func(p *NewExternalIdentityProps) { p.OrganizationID = "" }, nil},
		{"principal as organization", func(p *NewExternalIdentityProps) { p.OrganizationID = testPrincipal }, nil},
		{"no principal", func(p *NewExternalIdentityProps) { p.PrincipalID = "" }, nil},
		{"account as principal", func(p *NewExternalIdentityProps) { p.PrincipalID = testAccount }, nil},
		{"no identity provider", func(p *NewExternalIdentityProps) { p.IdentityProviderID = "" }, nil},
		{"malformed identity provider", func(p *NewExternalIdentityProps) { p.IdentityProviderID = "idp_nope" }, nil},
		{"no subject", func(p *NewExternalIdentityProps) { p.Subject = "" }, nil},
		{"subject too long", func(p *NewExternalIdentityProps) { p.Subject = strings.Repeat("a", maxSubjectLength+1) }, nil},
		{"ID with wrong prefix", func(*NewExternalIdentityProps) {}, new(id.New("idp"))},
		{"malformed ID", func(*NewExternalIdentityProps) {}, new("xid_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := xidProps()
			tt.mutate(&props)
			_, err := NewExternalIdentity(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("subject at the limit", func(t *testing.T) {
		props := xidProps()
		props.Subject = strings.Repeat("a", maxSubjectLength)
		_, err := NewExternalIdentity(props, nil)
		require.NoError(t, err)
	})
}

func TestExternalIdentityRecordLogin(t *testing.T) {
	clock.Fix(t, t0)
	e, err := NewExternalIdentity(xidProps(), nil)
	require.NoError(t, err)
	clock.Fix(t, t1)

	e.RecordLogin()

	require.NotNil(t, e.LastLoginAt())
	assert.Equal(t, t1, *e.LastLoginAt())
	assert.Equal(t, t0, e.CreatedAt())
}

func TestExternalIdentityGettersReturnCopies(t *testing.T) {
	props := xidProps()
	props.LastLoginAt = new(t0)
	e, err := NewExternalIdentity(props, nil)
	require.NoError(t, err)

	*e.LastLoginAt() = t2
	snap := e.Snapshot()
	*snap.LastLoginAt = t2

	assert.Equal(t, t0, *e.LastLoginAt())
}

func TestExternalIdentitySnapshot(t *testing.T) {
	clock.Fix(t, t0)
	e, err := NewExternalIdentity(xidProps(), nil)
	require.NoError(t, err)

	assert.Equal(t, ExternalIdentitySnapshot{
		ID:                 e.ID(),
		OrganizationID:     testOrg,
		PrincipalID:        testPrincipal,
		IdentityProviderID: testIdP,
		Subject:            "248289761001",
		CreatedAt:          t0,
	}, e.Snapshot())
}

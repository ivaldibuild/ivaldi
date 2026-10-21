package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const grantRole = "rol_00000000000000000000000003"

func grantProps() NewGrantProps {
	return NewGrantProps{
		OrganizationID:       testOrg,
		PrincipalID:          new(testPrincipal),
		RoleID:               grantRole,
		ResourceID:           testWorkspace,
		GrantedByPrincipalID: testCreator,
	}
}

func grantNew(t *testing.T) *Grant {
	t.Helper()
	g, err := NewGrant(grantProps(), nil)
	require.NoError(t, err)
	return g
}

func TestNewGrant(t *testing.T) {
	clock.Fix(t, t0)

	g := grantNew(t)

	assert.True(t, id.Valid("grt", g.ID()), "a new grant gets a grt_ ID: %q", g.ID())
	assert.Equal(t, testOrg, g.OrganizationID())
	require.NotNil(t, g.PrincipalID())
	assert.Equal(t, testPrincipal, *g.PrincipalID())
	assert.Nil(t, g.GroupID())
	assert.Equal(t, grantRole, g.RoleID())
	assert.Equal(t, testWorkspace, g.ResourceID())
	assert.Equal(t, testCreator, g.GrantedByPrincipalID())
	assert.Nil(t, g.ExpiresAt())
	assert.Equal(t, t0, g.CreatedAt())
	assert.False(t, g.Expired())
}

func TestNewGrantToGroup(t *testing.T) {
	props := grantProps()
	props.PrincipalID = nil
	props.GroupID = new(testGroup)

	g, err := NewGrant(props, nil)

	require.NoError(t, err)
	assert.Nil(t, g.PrincipalID())
	require.NotNil(t, g.GroupID())
	assert.Equal(t, testGroup, *g.GroupID())
}

func TestNewGrantOnOrganization(t *testing.T) {
	props := grantProps()
	props.ResourceID = testOrg

	g, err := NewGrant(props, nil)

	require.NoError(t, err, "the Organization's root resource uses its own ID")
	assert.Equal(t, testOrg, g.ResourceID())
}

func TestNewGrantWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("grt")
	props := grantProps()
	props.CreatedAt = new(t0)
	props.ExpiresAt = new(t1)

	g, err := NewGrant(props, &given)

	require.NoError(t, err, "an expired grant still loads")
	assert.Equal(t, given, g.ID())
	assert.Equal(t, t0, g.CreatedAt())
	require.NotNil(t, g.ExpiresAt())
	assert.Equal(t, t1, *g.ExpiresAt())
	assert.True(t, g.Expired())

	*props.ExpiresAt = t2
	assert.Equal(t, t1, *g.ExpiresAt(), "the given time is copied")
}

func TestNewGrantIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewGrantProps)
		id     *string
	}{
		{"no organization", func(p *NewGrantProps) { p.OrganizationID = "" }, nil},
		{"organization with wrong prefix", func(p *NewGrantProps) { p.OrganizationID = testWorkspace }, nil},
		{"no grantee", func(p *NewGrantProps) { p.PrincipalID = nil }, nil},
		{"empty grantees", func(p *NewGrantProps) { p.PrincipalID = new(""); p.GroupID = new("") }, nil},
		{"both grantees", func(p *NewGrantProps) { p.GroupID = new(testGroup) }, nil},
		{"principal with wrong prefix", func(p *NewGrantProps) { p.PrincipalID = new(testAccount) }, nil},
		{"group with wrong prefix", func(p *NewGrantProps) { p.PrincipalID = nil; p.GroupID = new(testPrincipal) }, nil},
		{"no role", func(p *NewGrantProps) { p.RoleID = "" }, nil},
		{"role key instead of ID", func(p *NewGrantProps) { p.RoleID = RoleMember }, nil},
		{"no resource", func(p *NewGrantProps) { p.ResourceID = "" }, nil},
		{"resource ID too long", func(p *NewGrantProps) { p.ResourceID = strings.Repeat("r", maxReferenceLength+1) }, nil},
		{"no granter", func(p *NewGrantProps) { p.GrantedByPrincipalID = "" }, nil},
		{"granter with wrong prefix", func(p *NewGrantProps) { p.GrantedByPrincipalID = testGroup }, nil},
		{"expiry at creation", func(p *NewGrantProps) { p.CreatedAt = new(t1); p.ExpiresAt = new(t1) }, nil},
		{"expiry before creation", func(p *NewGrantProps) { p.CreatedAt = new(t1); p.ExpiresAt = new(t0) }, nil},
		{"ID with wrong prefix", func(*NewGrantProps) {}, new(id.New("grp"))},
		{"malformed ID", func(*NewGrantProps) {}, new("grt_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := grantProps()
			tt.mutate(&props)
			_, err := NewGrant(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("empty group is none", func(t *testing.T) {
		props := grantProps()
		props.GroupID = new("")
		g, err := NewGrant(props, nil)
		require.NoError(t, err)
		assert.Nil(t, g.GroupID())
	})
}

func TestGrantExpired(t *testing.T) {
	clock.Fix(t, t0)
	props := grantProps()
	props.ExpiresAt = new(t1)
	g, err := NewGrant(props, nil)
	require.NoError(t, err)

	assert.False(t, g.Expired())
	clock.Fix(t, t1)
	assert.True(t, g.Expired(), "a grant has expired at its expiry time")
	clock.Fix(t, t2)
	assert.True(t, g.Expired())

	assert.False(t, grantNew(t).Expired(), "a grant without expiry never expires")
}

func TestGrantGettersReturnCopies(t *testing.T) {
	clock.Fix(t, t0)
	props := grantProps()
	props.ExpiresAt = new(t2)
	g, err := NewGrant(props, nil)
	require.NoError(t, err)

	*g.PrincipalID() = testCreator
	*g.ExpiresAt() = t0
	snap := g.Snapshot()
	*snap.PrincipalID = testCreator
	*snap.ExpiresAt = t0
	*props.PrincipalID = testCreator

	assert.Equal(t, testPrincipal, *g.PrincipalID())
	assert.Equal(t, t2, *g.ExpiresAt())
}

func TestGrantSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	props := grantProps()
	props.ExpiresAt = new(t2)
	g, err := NewGrant(props, nil)
	require.NoError(t, err)

	assert.Equal(t, GrantSnapshot{
		ID:                   g.ID(),
		OrganizationID:       testOrg,
		PrincipalID:          new(testPrincipal),
		RoleID:               grantRole,
		ResourceID:           testWorkspace,
		GrantedByPrincipalID: testCreator,
		ExpiresAt:            new(t2),
		CreatedAt:            t0,
	}, g.Snapshot())
}

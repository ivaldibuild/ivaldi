package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func groupManualProps() NewGroupProps {
	return NewGroupProps{OrganizationID: testOrg, Name: "Platform", Source: GroupManual}
}

func groupSCIMProps() NewGroupProps {
	return NewGroupProps{OrganizationID: testOrg, Name: "Engineering", Source: GroupSCIM, ExternalID: new("00g1abcd")}
}

func groupNew(t *testing.T) *Group {
	t.Helper()
	g, err := NewGroup(groupManualProps(), nil)
	require.NoError(t, err)
	return g
}

func TestNewGroup(t *testing.T) {
	clock.Fix(t, t0)

	g := groupNew(t)

	assert.True(t, id.Valid("grp", g.ID()), "a new group gets a grp_ ID: %q", g.ID())
	assert.Equal(t, testOrg, g.OrganizationID())
	assert.Equal(t, "Platform", g.Name())
	assert.Equal(t, GroupManual, g.Source())
	assert.Nil(t, g.ExternalID())
	assert.Equal(t, t0, g.CreatedAt())
	assert.Equal(t, t0, g.UpdatedAt())
}

func TestNewGroupSCIM(t *testing.T) {
	g, err := NewGroup(groupSCIMProps(), nil)

	require.NoError(t, err)
	assert.Equal(t, GroupSCIM, g.Source())
	require.NotNil(t, g.ExternalID())
	assert.Equal(t, "00g1abcd", *g.ExternalID())
}

func TestNewGroupWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("grp")
	props := groupManualProps()
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)

	g, err := NewGroup(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, g.ID())
	assert.Equal(t, t0, g.CreatedAt())
	assert.Equal(t, t1, g.UpdatedAt())
}

func TestNewGroupIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		props  func() NewGroupProps
		mutate func(*NewGroupProps)
		id     *string
	}{
		{"no organization", groupManualProps, func(p *NewGroupProps) { p.OrganizationID = "" }, nil},
		{"organization with wrong prefix", groupManualProps, func(p *NewGroupProps) { p.OrganizationID = testWorkspace }, nil},
		{"no name", groupManualProps, func(p *NewGroupProps) { p.Name = "" }, nil},
		{"blank name", groupManualProps, func(p *NewGroupProps) { p.Name = " " }, nil},
		{"name too long", groupManualProps, func(p *NewGroupProps) { p.Name = strings.Repeat("é", maxNameLength+1) }, nil},
		{"no source", groupManualProps, func(p *NewGroupProps) { p.Source = "" }, nil},
		{"unknown source", groupManualProps, func(p *NewGroupProps) { p.Source = "team" }, nil},
		{"manual with external ID", groupManualProps, func(p *NewGroupProps) { p.ExternalID = new("00g1abcd") }, nil},
		{"SCIM without external ID", groupSCIMProps, func(p *NewGroupProps) { p.ExternalID = nil }, nil},
		{"SCIM with empty external ID", groupSCIMProps, func(p *NewGroupProps) { p.ExternalID = new("") }, nil},
		{"SCIM external ID too long", groupSCIMProps, func(p *NewGroupProps) { p.ExternalID = new(strings.Repeat("x", maxExternalIDLength+1)) }, nil},
		{"ID with wrong prefix", groupManualProps, func(*NewGroupProps) {}, new(id.New("rol"))},
		{"malformed ID", groupManualProps, func(*NewGroupProps) {}, new("grp_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := tt.props()
			tt.mutate(&props)
			_, err := NewGroup(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("manual with empty external ID is none", func(t *testing.T) {
		props := groupManualProps()
		props.ExternalID = new("")
		g, err := NewGroup(props, nil)
		require.NoError(t, err)
		assert.Nil(t, g.ExternalID())
	})
}

func TestGroupGettersReturnCopies(t *testing.T) {
	props := groupSCIMProps()
	g, err := NewGroup(props, nil)
	require.NoError(t, err)

	*g.ExternalID() = "changed"
	snap := g.Snapshot()
	*snap.ExternalID = "changed"
	*props.ExternalID = "changed"

	assert.Equal(t, "00g1abcd", *g.ExternalID())
}

func TestGroupSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	g, err := NewGroup(groupSCIMProps(), nil)
	require.NoError(t, err)

	assert.Equal(t, GroupSnapshot{
		ID:             g.ID(),
		OrganizationID: testOrg,
		Name:           "Engineering",
		Source:         GroupSCIM,
		ExternalID:     new("00g1abcd"),
		CreatedAt:      t0,
		UpdatedAt:      t0,
	}, g.Snapshot())
}

func TestGroupRename(t *testing.T) {
	clock.Fix(t, t0)
	g := groupNew(t)

	clock.Fix(t, t1)
	require.NoError(t, g.Rename("Platform team"))

	assert.Equal(t, "Platform team", g.Name())
	assert.Equal(t, t1, g.UpdatedAt())
	assert.Equal(t, t0, g.CreatedAt())

	clock.Fix(t, t2)
	assert.ErrorIs(t, g.Rename(""), ErrInvalid)
	assert.Equal(t, "Platform team", g.Name(), "a refused rename changes nothing")
	assert.Equal(t, t1, g.UpdatedAt())
}

func groupMemberProps() NewGroupMemberProps {
	return NewGroupMemberProps{OrganizationID: testOrg, GroupID: testGroup, PrincipalID: testPrincipal}
}

func TestNewGroupMember(t *testing.T) {
	clock.Fix(t, t0)

	m, err := NewGroupMember(groupMemberProps())

	require.NoError(t, err)
	assert.Equal(t, testOrg, m.OrganizationID())
	assert.Equal(t, testGroup, m.GroupID())
	assert.Equal(t, testPrincipal, m.PrincipalID())
	assert.Equal(t, t0, m.CreatedAt())
	assert.Equal(t, GroupMemberSnapshot{
		OrganizationID: testOrg,
		GroupID:        testGroup,
		PrincipalID:    testPrincipal,
		CreatedAt:      t0,
	}, m.Snapshot())
}

func TestNewGroupMemberWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	props := groupMemberProps()
	props.CreatedAt = new(t0)

	m, err := NewGroupMember(props)

	require.NoError(t, err)
	assert.Equal(t, t0, m.CreatedAt())
}

func TestNewGroupMemberIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewGroupMemberProps)
	}{
		{"no organization", func(p *NewGroupMemberProps) { p.OrganizationID = "" }},
		{"organization with wrong prefix", func(p *NewGroupMemberProps) { p.OrganizationID = testGroup }},
		{"no group", func(p *NewGroupMemberProps) { p.GroupID = "" }},
		{"group with wrong prefix", func(p *NewGroupMemberProps) { p.GroupID = testPrincipal }},
		{"no principal", func(p *NewGroupMemberProps) { p.PrincipalID = "" }},
		{"account instead of principal", func(p *NewGroupMemberProps) { p.PrincipalID = testAccount }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := groupMemberProps()
			tt.mutate(&props)
			_, err := NewGroupMember(props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

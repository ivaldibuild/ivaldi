package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func roleBuiltIn(t *testing.T, key string) *Role {
	t.Helper()
	roleID, ok := BuiltInRoleID(key)
	require.True(t, ok, key)
	r, err := NewRole(NewRoleProps{Key: key, Name: strings.ToUpper(key[:1]) + key[1:]}, &roleID)
	require.NoError(t, err)
	return r
}

func roleCustomProps() NewRoleProps {
	return NewRoleProps{OrganizationID: new(testOrg), Key: "release-manager", Name: "Release manager"}
}

func roleCustom(t *testing.T) *Role {
	t.Helper()
	r, err := NewRole(roleCustomProps(), nil)
	require.NoError(t, err)
	return r
}

func TestBuiltInRoleID(t *testing.T) {
	// These IDs are seeded by migration 000020; changing one breaks every installation.
	want := map[string]string{
		RoleOwner:  "rol_00000000000000000000000001",
		RoleAdmin:  "rol_00000000000000000000000002",
		RoleMember: "rol_00000000000000000000000003",
		RoleViewer: "rol_00000000000000000000000004",
	}
	for key, roleID := range want {
		got, ok := BuiltInRoleID(key)
		assert.True(t, ok, key)
		assert.Equal(t, roleID, got, key)
		assert.True(t, id.Valid("rol", got), "the seeded ID %s is a valid rol_ ID", got)
	}

	_, ok := BuiltInRoleID("release-manager")
	assert.False(t, ok)
}

func TestNewRoleBuiltIn(t *testing.T) {
	clock.Fix(t, t0)

	r := roleBuiltIn(t, RoleOwner)

	assert.Equal(t, "rol_00000000000000000000000001", r.ID())
	assert.Nil(t, r.OrganizationID())
	assert.Equal(t, RoleOwner, r.Key())
	assert.Equal(t, "Owner", r.Name())
	assert.Equal(t, t0, r.CreatedAt())
	assert.True(t, r.BuiltIn())
}

func TestNewRoleCustom(t *testing.T) {
	clock.Fix(t, t0)

	r := roleCustom(t)

	assert.True(t, id.Valid("rol", r.ID()), "a new custom role gets an rol_ ID: %q", r.ID())
	require.NotNil(t, r.OrganizationID())
	assert.Equal(t, testOrg, *r.OrganizationID())
	assert.Equal(t, "release-manager", r.Key())
	assert.Equal(t, "Release manager", r.Name())
	assert.Equal(t, t0, r.CreatedAt())
	assert.False(t, r.BuiltIn())
}

func TestNewRoleWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("rol")
	props := roleCustomProps()
	props.CreatedAt = new(t0)

	r, err := NewRole(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, r.ID())
	assert.Equal(t, t0, r.CreatedAt())
}

func TestNewRoleIsRefused(t *testing.T) {
	ownerID, _ := BuiltInRoleID(RoleOwner)
	adminID, _ := BuiltInRoleID(RoleAdmin)
	tests := []struct {
		name  string
		props NewRoleProps
		id    *string
	}{
		{"built-in without its ID", NewRoleProps{Key: RoleOwner, Name: "Owner"}, nil},
		{"built-in with a new ID", NewRoleProps{Key: RoleOwner, Name: "Owner"}, new(id.New("rol"))},
		{"built-in with another role's ID", NewRoleProps{Key: RoleOwner, Name: "Owner"}, &adminID},
		{"unknown key without organization", NewRoleProps{Key: "release-manager", Name: "Release manager"}, nil},
		{"empty organization is none", NewRoleProps{OrganizationID: new(""), Key: "release-manager", Name: "Release manager"}, nil},
		{"custom with built-in key", NewRoleProps{OrganizationID: new(testOrg), Key: RoleAdmin, Name: "Admin"}, nil},
		{"custom with built-in ID and key", NewRoleProps{OrganizationID: new(testOrg), Key: RoleOwner, Name: "Owner"}, &ownerID},
		{"organization with wrong prefix", NewRoleProps{OrganizationID: new(testWorkspace), Key: "release-manager", Name: "Release manager"}, nil},
		{"uppercase key", NewRoleProps{OrganizationID: new(testOrg), Key: "Release", Name: "Release"}, nil},
		{"empty key", NewRoleProps{OrganizationID: new(testOrg), Key: "", Name: "Release"}, nil},
		{"key with underscore", NewRoleProps{OrganizationID: new(testOrg), Key: "release_manager", Name: "Release"}, nil},
		{"no name", NewRoleProps{OrganizationID: new(testOrg), Key: "release-manager", Name: ""}, nil},
		{"name too long", NewRoleProps{OrganizationID: new(testOrg), Key: "release-manager", Name: strings.Repeat("é", maxNameLength+1)}, nil},
		{"built-in without name", NewRoleProps{Key: RoleOwner}, &ownerID},
		{"ID with wrong prefix", roleCustomProps(), new(id.New("grp"))},
		{"malformed ID", roleCustomProps(), new("rol_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRole(tt.props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestRoleCheckGrantableAt(t *testing.T) {
	all := []ResourceType{ResourceOrganization, ResourceWorkspace, ResourceProject, ResourceSource, ResourceEnvironment}
	tests := []struct {
		key     string
		allowed []ResourceType
	}{
		{RoleOwner, []ResourceType{ResourceOrganization}},
		{RoleAdmin, []ResourceType{ResourceOrganization, ResourceWorkspace}},
		{RoleMember, all},
		{RoleViewer, all},
	}
	for _, tt := range tests {
		r := roleBuiltIn(t, tt.key)
		for _, rt := range all {
			t.Run(tt.key+" on "+string(rt), func(t *testing.T) {
				err := r.CheckGrantableAt(rt)
				for _, a := range tt.allowed {
					if a == rt {
						assert.NoError(t, err)
						return
					}
				}
				assert.ErrorIs(t, err, ErrInvalid)
			})
		}
	}

	t.Run("custom roles anywhere", func(t *testing.T) {
		r := roleCustom(t)
		for _, rt := range all {
			assert.NoError(t, r.CheckGrantableAt(rt), string(rt))
		}
	})
}

func TestRoleAvailableIn(t *testing.T) {
	builtIn := roleBuiltIn(t, RoleViewer)
	custom := roleCustom(t)

	assert.True(t, builtIn.AvailableIn(testOrg))
	assert.True(t, builtIn.AvailableIn(otherOrg))
	assert.True(t, custom.AvailableIn(testOrg))
	assert.False(t, custom.AvailableIn(otherOrg), "a custom role is only available in its own organization")
}

func TestRoleGettersReturnCopies(t *testing.T) {
	props := roleCustomProps()
	r, err := NewRole(props, nil)
	require.NoError(t, err)

	*r.OrganizationID() = otherOrg
	snap := r.Snapshot()
	*snap.OrganizationID = otherOrg
	*props.OrganizationID = otherOrg

	assert.Equal(t, testOrg, *r.OrganizationID())
}

func TestRoleSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	r := roleCustom(t)

	assert.Equal(t, RoleSnapshot{
		ID:             r.ID(),
		OrganizationID: new(testOrg),
		Key:            "release-manager",
		Name:           "Release manager",
		CreatedAt:      t0,
	}, r.Snapshot())
}

func TestRoleRename(t *testing.T) {
	r := roleCustom(t)

	require.NoError(t, r.Rename("Release lead"))
	assert.Equal(t, "Release lead", r.Name())

	assert.ErrorIs(t, r.Rename(""), ErrInvalid)
	assert.ErrorIs(t, r.Rename(strings.Repeat("é", maxNameLength+1)), ErrInvalid)
	assert.Equal(t, "Release lead", r.Name(), "a refused rename changes nothing")
}

func TestRoleRenameBuiltInIsRefused(t *testing.T) {
	r := roleBuiltIn(t, RoleAdmin)

	assert.ErrorIs(t, r.Rename("Administrator"), ErrBuiltInRole)
	assert.Equal(t, "Admin", r.Name())
}

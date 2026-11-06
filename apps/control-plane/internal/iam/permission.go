package iam

import "strings"

// Permissions are named "<resource>.<verb>", such as "environment.read" or
// "grant.manage". Each capability declares the permissions it checks in its
// constants.go; the built-in roles below decide who holds them.
//
// The built-in roles are defined by rule rather than by listing every
// permission, so a capability's new permission is covered without changing
// IAM:
//
//   - owner holds every permission.
//   - admin holds every permission except the owner-only ones below.
//   - member holds every read permission, plus memberPermissions.
//   - viewer holds every read permission.
//
// A read permission is one whose verb is "read". Custom roles hold no
// permissions until their permissions can be stored.

// Owner-only permissions: what the design reserves for owners. No capability
// checks them yet.
const (
	PermissionOrganizationDelete     = "organization.delete"
	PermissionOwnerManage            = "owner.manage"
	PermissionIdentityProviderManage = "identity_provider.manage"
	PermissionBillingManage          = "billing.manage"
)

var ownerOnlyPermissions = map[string]bool{
	PermissionOrganizationDelete:     true,
	PermissionOwnerManage:            true,
	PermissionIdentityProviderManage: true,
	PermissionBillingManage:          true,
}

// memberPermissions are what a member may do beyond reading: work on Work
// Items and run Sessions. The capabilities that check them do not exist yet.
var memberPermissions = map[string]bool{
	"work_item.create": true,
	"work_item.manage": true,
	"session.create":   true,
}

// IsReadPermission reports whether permission only reads.
func IsReadPermission(permission string) bool {
	return strings.HasSuffix(permission, ".read")
}

// BuiltInRoleAllows reports whether the built-in role with key holds
// permission. An unknown key holds nothing.
func BuiltInRoleAllows(key, permission string) bool {
	switch key {
	case RoleOwner:
		return true
	case RoleAdmin:
		return !ownerOnlyPermissions[permission]
	case RoleMember:
		return IsReadPermission(permission) || memberPermissions[permission]
	case RoleViewer:
		return IsReadPermission(permission)
	}
	return false
}

// BuiltInRoleKey returns the key of the built-in role with the fixed ID, and
// whether there is one.
func BuiltInRoleKey(roleID string) (string, bool) {
	for key, r := range builtInRoles {
		if r.id == roleID {
			return key, true
		}
	}
	return "", false
}

package iam

// Profile is who a signed-in caller is: their principal, the Organization
// they act in, and the roles they hold on it. It is read, never stored.
type Profile struct {
	Principal PrincipalSnapshot
	// Email is the Account's email. Only a user has one.
	Email        *string
	Organization OrganizationSnapshot
	// Roles are the unexpired roles granted on the Organization itself, to
	// the principal or to a group it is in, sorted by role ID, which puts
	// the built-in roles from owner to viewer.
	Roles []HeldRole
}

// HeldRole is a role a principal holds on a resource, granted to it
// directly or to a group it is in.
type HeldRole struct {
	// Key is the built-in role's key, such as RoleOwner, or empty for a
	// custom role.
	Key        string
	RoleID     string
	ResourceID string
}

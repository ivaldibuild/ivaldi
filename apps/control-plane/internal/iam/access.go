package iam

import "time"

// AccessQuery asks what a principal holds on a resource, for an
// authorization check.
type AccessQuery struct {
	OrganizationID string
	PrincipalID    string
	ResourceID     string
	// At is the time grants must not have expired by.
	At time.Time
}

// Access is what an authorization check needs to know about a principal and
// a resource, read in one go.
type Access struct {
	// OrganizationActive is false when the Organization is suspended,
	// deleted, or does not exist.
	OrganizationActive bool
	// PrincipalStatus is the principal's status in the Organization, or
	// empty when it is not one of the Organization's principals.
	PrincipalStatus PrincipalStatus
	// ResourceFound is false when the resource is not one of the
	// Organization's, whether or not it is archived.
	ResourceFound bool
	// RoleIDs are the roles granted, unexpired, to the principal or a group
	// it is in, on the resource or an ancestor. Grants on an archived
	// resource do not count, but grants on its active ancestors do.
	RoleIDs []string
}

// HeldResourcesQuery asks on which resources a principal holds a
// permission, such as to list what it may decide without checking each
// item in turn.
type HeldResourcesQuery struct {
	OrganizationID string
	PrincipalID    string
	Permission     string
	// At is the time grants must not have expired by.
	At time.Time
}

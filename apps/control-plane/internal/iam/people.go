package iam

import "github.com/ivaldibuild/ivaldi/pkg/pagination"

// Person is one of an Organization's users, as a list of its people shows
// them. It is read, never stored.
type Person struct {
	User UserSnapshot
	// Email is the user's Account email.
	Email string
	// Roles are the unexpired roles the user holds on the Organization
	// itself, granted directly or to a group they are in, sorted by role
	// ID. Roles held only on a Workspace or below are not listed.
	Roles []HeldRole
}

// ListPeopleQuery lists an Organization's users.
type ListPeopleQuery struct {
	OrganizationID string
	// Search keeps the people whose display name or email contains it,
	// ignoring case. Empty keeps everyone.
	Search string
	// IncludeInactive also lists suspended and deactivated users.
	IncludeInactive bool
	// Page sorts by "display_name" (the default) or "created_at", with the
	// principal ID breaking ties.
	Page pagination.Query
}

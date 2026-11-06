package iam

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// GrantEntry is a Grant as a list of grants shows it: with its grantee and
// role named. It is read, never stored.
type GrantEntry struct {
	Grant GrantSnapshot
	// RoleKey is the built-in role's key, or empty for a custom role.
	RoleKey string
	// Principal is the grantee when the Grant is to a principal.
	Principal *GranteePrincipal
	// Group is the grantee when the Grant is to a group.
	Group *GranteeGroup
}

// GranteePrincipal names the principal a Grant is to.
type GranteePrincipal struct {
	ID          string
	Kind        PrincipalKind
	DisplayName string
	Status      PrincipalStatus
}

// GranteeGroup names the group a Grant is to. A Team's group has Source
// GroupTeam and the Team's ID as its ExternalID.
type GranteeGroup struct {
	ID         string
	Name       string
	Source     GroupSource
	ExternalID *string
}

// ListGrantsQuery lists the grants on resources.
type ListGrantsQuery struct {
	OrganizationID string
	// ResourceIDs are the resources whose grants are listed.
	ResourceIDs []string
	// GrantID, when set, keeps only that grant.
	GrantID string
	// IncludeExpired also lists grants that have expired by At.
	IncludeExpired bool
	At             time.Time
	// Page sorts by "created_at" (the default), with the grant ID breaking ties.
	Page pagination.Query
}

// ListResourceGrantsQuery asks for the grants on one resource, and
// optionally on the resources above it, which apply to it too.
type ListResourceGrantsQuery struct {
	OrganizationID   string
	ResourceID       string
	IncludeAncestors bool
	IncludeExpired   bool
	Page             pagination.Query
}

// CreateGrantInput grants a role on a resource to exactly one of a
// principal, a group, or a Team's group.
type CreateGrantInput struct {
	OrganizationID string
	PrincipalID    string
	GroupID        string
	// TeamID grants to the group that mirrors the Team, so access follows
	// the Team's members.
	TeamID string
	// Role is a built-in role's key: owner, admin, member, or viewer.
	Role       string
	ResourceID string
	// ExpiresAt is nil for a grant that does not expire.
	ExpiresAt *time.Time
}

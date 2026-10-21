package iam

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Grant gives a Role to exactly one principal or one Group, on one
// resource and everything beneath it. Grants only allow; restrictions
// belong to policy. A grant does not change: it is removed and granted again.
type Grant struct {
	id                   string
	organizationID       string
	principalID          *string
	groupID              *string
	roleID               string
	resourceID           string
	grantedByPrincipalID string
	expiresAt            *time.Time
	createdAt            time.Time
}

// GrantSnapshot is a Grant's state, for repositories and audit records.
type GrantSnapshot struct {
	ID                   string
	OrganizationID       string
	PrincipalID          *string
	GroupID              *string
	RoleID               string
	ResourceID           string
	GrantedByPrincipalID string
	ExpiresAt            *time.Time
	CreatedAt            time.Time
}

// NewGrantProps describes a Grant. A new one leaves CreatedAt nil; a
// repository loading a stored one sets it.
//
// Whether the Role may be granted on the resource is checked with
// Role.CheckGrantableAt and Role.AvailableIn before the Grant is made.
type NewGrantProps struct {
	OrganizationID string
	// Exactly one of PrincipalID and GroupID is set.
	PrincipalID          *string
	GroupID              *string
	RoleID               string
	ResourceID           string
	GrantedByPrincipalID string
	// ExpiresAt is nil for a grant that does not expire.
	ExpiresAt *time.Time
	CreatedAt *time.Time
}

// NewGrant returns a Grant, with a new ID unless grantID gives a stored one.
func NewGrant(props NewGrantProps, grantID *string) (*Grant, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateOwnID(Grant{}.Prefix(), grantID); err != nil {
		return nil, err
	}
	principalID := optionalString(props.PrincipalID)
	groupID := optionalString(props.GroupID)
	switch {
	case (principalID == nil) == (groupID == nil):
		return nil, NewErrInvalidInput("a grant needs exactly one grantee: a principal or a group")
	case principalID != nil:
		if err := validateID("principal ID", principalPrefix, *principalID); err != nil {
			return nil, err
		}
	default:
		if err := validateID("group ID", Group{}.Prefix(), *groupID); err != nil {
			return nil, err
		}
	}
	if err := validateID("role ID", Role{}.Prefix(), props.RoleID); err != nil {
		return nil, err
	}
	if err := validateReference("resource ID", props.ResourceID); err != nil {
		return nil, err
	}
	if err := validateID("granter", principalPrefix, props.GrantedByPrincipalID); err != nil {
		return nil, err
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateOptionalExpiry(props.ExpiresAt, createdAt); err != nil {
		return nil, err
	}

	g := &Grant{
		organizationID:       props.OrganizationID,
		principalID:          principalID,
		groupID:              groupID,
		roleID:               props.RoleID,
		resourceID:           props.ResourceID,
		grantedByPrincipalID: props.GrantedByPrincipalID,
		expiresAt:            clock.CopyTime(props.ExpiresAt),
		createdAt:            createdAt,
	}
	if grantID != nil {
		g.id = *grantID
	}
	id.Assign(&g.id, g)
	return g, nil
}

func (Grant) Prefix() string                  { return "grt" }
func (g *Grant) ID() string                   { return g.id }
func (g *Grant) OrganizationID() string       { return g.organizationID }
func (g *Grant) PrincipalID() *string         { return copyString(g.principalID) }
func (g *Grant) GroupID() *string             { return copyString(g.groupID) }
func (g *Grant) RoleID() string               { return g.roleID }
func (g *Grant) ResourceID() string           { return g.resourceID }
func (g *Grant) GrantedByPrincipalID() string { return g.grantedByPrincipalID }
func (g *Grant) ExpiresAt() *time.Time        { return clock.CopyTime(g.expiresAt) }
func (g *Grant) CreatedAt() time.Time         { return g.createdAt }

// Expired reports whether the grant no longer applies.
func (g *Grant) Expired() bool {
	return g.expiresAt != nil && !clock.Now().Before(*g.expiresAt)
}

func (g *Grant) Snapshot() GrantSnapshot {
	return GrantSnapshot{
		ID:                   g.id,
		OrganizationID:       g.organizationID,
		PrincipalID:          copyString(g.principalID),
		GroupID:              copyString(g.groupID),
		RoleID:               g.roleID,
		ResourceID:           g.resourceID,
		GrantedByPrincipalID: g.grantedByPrincipalID,
		ExpiresAt:            clock.CopyTime(g.expiresAt),
		CreatedAt:            g.createdAt,
	}
}

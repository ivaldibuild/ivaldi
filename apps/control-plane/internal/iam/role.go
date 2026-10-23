package iam

import (
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// Keys of the built-in roles, which every installation has.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// builtInRole is a role seeded by migration 000020, with a fixed ID so
// every installation agrees on it.
type builtInRole struct {
	id   string
	name string
	// grantableAt lists the resource types it may be granted on; nil means any.
	grantableAt []ResourceType
}

var builtInRoles = map[string]builtInRole{
	RoleOwner:  {id: "rol_00000000000000000000000001", name: "Owner", grantableAt: []ResourceType{ResourceOrganization}},
	RoleAdmin:  {id: "rol_00000000000000000000000002", name: "Admin", grantableAt: []ResourceType{ResourceOrganization, ResourceWorkspace}},
	RoleMember: {id: "rol_00000000000000000000000003", name: "Member"},
	RoleViewer: {id: "rol_00000000000000000000000004", name: "Viewer"},
}

// BuiltInRoleID returns the fixed ID of the built-in role with key, and
// whether there is one.
func BuiltInRoleID(key string) (string, bool) {
	r, ok := builtInRoles[key]
	return r.id, ok
}

// Role is a set of permissions that a Grant gives on a resource. Built-in
// roles belong to no Organization, and their permissions are defined in
// code. A custom role belongs to one Organization.
type Role struct {
	aggregate.Root
	organizationID *string
	key            string
	name           string
	createdAt      time.Time
}

// RoleSnapshot is a Role's state, for repositories and audit records.
type RoleSnapshot struct {
	ID             string
	OrganizationID *string
	Key            string
	Name           string
	CreatedAt      time.Time
}

// NewRoleProps describes a Role. A new one leaves CreatedAt nil; a
// repository loading a stored one sets it.
type NewRoleProps struct {
	// OrganizationID is nil for a built-in role.
	OrganizationID *string
	// Key is unique within the Organization, or among built-in roles.
	Key       string
	Name      string
	CreatedAt *time.Time
}

// NewRole returns a Role, with a new ID unless roleID gives a stored one. A
// built-in role must have its key and fixed ID.
func NewRole(props NewRoleProps, roleID *string) (*Role, error) {
	root, err := newRoot(Role{}.Prefix(), roleID)
	if err != nil {
		return nil, err
	}
	org := optionalString(props.OrganizationID)
	builtIn, isBuiltInKey := builtInRoles[props.Key]
	if org == nil {
		if !isBuiltInKey {
			return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a built-in role; a custom role needs an organization", props.Key))
		}
		if roleID == nil || *roleID != builtIn.id {
			return nil, NewErrInvalidInput(fmt.Sprintf("the built-in %s role has the ID %s", props.Key, builtIn.id))
		}
	} else {
		if err := validateID("organization ID", Organization{}.Prefix(), *org); err != nil {
			return nil, err
		}
		if isBuiltInKey {
			return nil, NewErrInvalidInput(fmt.Sprintf("%q is the key of a built-in role", props.Key))
		}
		if err := validateSlug(props.Key); err != nil {
			return nil, err
		}
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}

	r := &Role{
		Root:           root,
		organizationID: org,
		key:            props.Key,
		name:           props.Name,
		createdAt:      timeOr(props.CreatedAt, clock.Now()),
	}
	return r, nil
}

func (Role) Prefix() string             { return "rol" }
func (r *Role) OrganizationID() *string { return copyString(r.organizationID) }
func (r *Role) Key() string             { return r.key }
func (r *Role) Name() string            { return r.name }
func (r *Role) CreatedAt() time.Time    { return r.createdAt }

func (r *Role) BuiltIn() bool { return r.organizationID == nil }

// CheckGrantableAt returns an error unless the role may be granted on a
// resource of type t: owner only on an Organization, and admin only on an
// Organization or Workspace. Custom roles may be granted anywhere.
func (r *Role) CheckGrantableAt(t ResourceType) error {
	if !r.BuiltIn() {
		return nil
	}
	allowed := builtInRoles[r.key].grantableAt
	if allowed != nil && !slices.Contains(allowed, t) {
		return NewErrInvalidInput(fmt.Sprintf("the %s role cannot be granted on a %s", r.key, t))
	}
	return nil
}

// AvailableIn reports whether the role can be granted in an Organization:
// built-in roles can be granted in every one, and a custom role only in its own.
func (r *Role) AvailableIn(organizationID string) bool {
	return r.BuiltIn() || *r.organizationID == organizationID
}

func (r *Role) Snapshot() RoleSnapshot {
	return RoleSnapshot{
		ID:             r.ID(),
		OrganizationID: copyString(r.organizationID),
		Key:            r.key,
		Name:           r.name,
		CreatedAt:      r.createdAt,
	}
}

// Rename changes a custom role's name.
func (r *Role) Rename(name string) error {
	if r.BuiltIn() {
		return fmt.Errorf("%w: %s", ErrBuiltInRole, r.key)
	}
	if err := validateName("name", name, maxNameLength); err != nil {
		return err
	}
	r.name = name
	return nil
}

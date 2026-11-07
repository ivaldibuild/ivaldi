package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// GroupSource says who manages a group's membership.
type GroupSource string

const (
	// GroupManual groups are managed by people in the Organization.
	GroupManual GroupSource = "manual"
	// GroupSCIM groups are provisioned from an identity provider through
	// SCIM, and identified there by their external ID.
	GroupSCIM GroupSource = "scim"
	// GroupTeam groups mirror a Team, so the Team can receive grants (ADR
	// 0007). Their external ID is the Team's ID, and only the team
	// capability changes their name and members, through TeamGroups.
	GroupTeam GroupSource = "team"
)

// Group is a set of principals in an Organization that can receive grants
// together. Its name is unique in the Organization. Members are
// GroupMembers, kept apart from the Group because a provisioned group can
// be large.
type Group struct {
	aggregate.Root
	organizationID string
	name           string
	source         GroupSource
	externalID     *string
	createdAt      time.Time
	updatedAt      time.Time
}

// GroupSnapshot is a Group's state, for repositories, audit records, and
// events. Its JSON form is part of the Group events' contract.
type GroupSnapshot struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organization_id"`
	Name           string      `json:"name"`
	Source         GroupSource `json:"source"`
	ExternalID     *string     `json:"external_id"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// NewGroupProps describes a Group. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewGroupProps struct {
	OrganizationID string
	Name           string
	Source         GroupSource
	// ExternalID is the identity provider's ID for a SCIM group, or the
	// Team's ID for a team group. It is required for both and not allowed
	// for a manual group.
	ExternalID *string
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

// NewGroup returns a Group, with a new ID unless groupID gives a stored one.
func NewGroup(props NewGroupProps, groupID *string) (*Group, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(Group{}.Prefix(), groupID)
	if err != nil {
		return nil, err
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}
	externalID := optional.OfPtr(props.ExternalID)
	switch props.Source {
	case GroupManual:
		if externalID != nil {
			return nil, NewErrInvalidInput("a manual group has no external ID")
		}
	case GroupSCIM, GroupTeam:
		if externalID == nil {
			return nil, NewErrInvalidInput(fmt.Sprintf("a %s group needs its external ID", props.Source))
		}
		if err := validateName("external ID", *externalID, maxExternalIDLength); err != nil {
			return nil, err
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("group source %q must be manual, scim, or team", props.Source))
	}

	now := clock.Now()
	g := &Group{
		Root:           root,
		organizationID: props.OrganizationID,
		name:           props.Name,
		source:         props.Source,
		externalID:     externalID,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	if g.IsNew() {
		g.RecordEvent(GroupCreated{g.Snapshot()})
	}
	return g, nil
}

func (Group) Prefix() string            { return "grp" }
func (g *Group) OrganizationID() string { return g.organizationID }
func (g *Group) Name() string           { return g.name }
func (g *Group) Source() GroupSource    { return g.source }
func (g *Group) ExternalID() *string    { return copyString(g.externalID) }
func (g *Group) CreatedAt() time.Time   { return g.createdAt }
func (g *Group) UpdatedAt() time.Time   { return g.updatedAt }

func (g *Group) Snapshot() GroupSnapshot {
	return GroupSnapshot{
		ID:             g.ID(),
		OrganizationID: g.organizationID,
		Name:           g.name,
		Source:         g.source,
		ExternalID:     copyString(g.externalID),
		CreatedAt:      g.createdAt,
		UpdatedAt:      g.updatedAt,
	}
}

// Rename changes the group's name.
func (g *Group) Rename(name string) error {
	if err := validateName("name", name, maxNameLength); err != nil {
		return err
	}
	if name != g.name {
		g.name = name
		g.RecordEvent(GroupRenamed{g.Snapshot()})
	}
	g.updatedAt = clock.Now()
	return nil
}

// GroupMember records that a principal belongs to a Group. It is
// identified by the Group and principal.
type GroupMember struct {
	aggregate.Events
	organizationID string
	groupID        string
	principalID    string
	createdAt      time.Time
}

// GroupMemberSnapshot is a GroupMember's state, for repositories, audit
// records, and events. Its JSON form is part of the GroupMember events'
// contract.
type GroupMemberSnapshot struct {
	OrganizationID string    `json:"organization_id"`
	GroupID        string    `json:"group_id"`
	PrincipalID    string    `json:"principal_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// NewGroupMemberProps describes a GroupMember. A new one leaves CreatedAt
// nil; a repository loading a stored one sets it.
type NewGroupMemberProps struct {
	OrganizationID string
	GroupID        string
	PrincipalID    string
	CreatedAt      *time.Time
}

// NewGroupMember returns a GroupMember.
func NewGroupMember(props NewGroupMemberProps) (*GroupMember, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateID("group ID", Group{}.Prefix(), props.GroupID); err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	m := &GroupMember{
		organizationID: props.OrganizationID,
		groupID:        props.GroupID,
		principalID:    props.PrincipalID,
		createdAt:      timeOr(props.CreatedAt, clock.Now()),
	}
	// A GroupMember has no ID to tell a new one from a stored one; a stored
	// one has its creation time.
	if props.CreatedAt == nil {
		m.RecordEvent(GroupMemberAdded{m.Snapshot()})
	}
	return m, nil
}

func (m *GroupMember) OrganizationID() string { return m.organizationID }
func (m *GroupMember) GroupID() string        { return m.groupID }
func (m *GroupMember) PrincipalID() string    { return m.principalID }
func (m *GroupMember) CreatedAt() time.Time   { return m.createdAt }

func (m *GroupMember) Snapshot() GroupMemberSnapshot {
	return GroupMemberSnapshot{
		OrganizationID: m.organizationID,
		GroupID:        m.groupID,
		PrincipalID:    m.principalID,
		CreatedAt:      m.createdAt,
	}
}

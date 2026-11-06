package team

// The interfaces this capability defines for itself: its repository, what
// it needs from IAM, and its service. Directory, the contract other
// capabilities depend on, is in directory.go.

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// ListTeamsQuery lists an Organization's Teams. It sorts by "created_at"
// (the default), "slug", or "display_name", with the ID breaking ties.
type ListTeamsQuery struct {
	OrganizationID  string
	IncludeArchived bool
	Page            pagination.Query
}

// TeamRepository persists and retrieves Teams with their members, and
// nothing more: IDs, timestamps, and every rule come from the domain. Every
// method is scoped to an Organization. Methods return ErrNotFound when no
// row matches, and ErrSlugTaken for a slug clash. List returns ErrInvalid
// for an unknown sort field or a cursor from another sort.
type TeamRepository interface {
	CreateTeam(ctx context.Context, t *Team) error
	GetTeam(ctx context.Context, organizationID, teamID string) (*Team, error)
	// GetTeamForUpdate is GetTeam that also locks the Team until the
	// transaction ends, so changes to its members do not overwrite each
	// other. It must be called inside a transaction.
	GetTeamForUpdate(ctx context.Context, organizationID, teamID string) (*Team, error)
	// GetTeamBySlug returns the active Team with the slug.
	GetTeamBySlug(ctx context.Context, organizationID, slug string) (*Team, error)
	ListTeams(ctx context.Context, query ListTeamsQuery) (*pagination.Result[*Team], error)
	// UpdateTeam writes the Team's state, including adding, changing, and
	// removing its members.
	UpdateTeam(ctx context.Context, t *Team) error
	// CountActiveTeamsWithMember counts the Organization's active Teams the
	// principal is a member of.
	CountActiveTeamsWithMember(ctx context.Context, organizationID, principalID string) (int, error)
}

// Principals finds the principals that may become members: active users and
// Agents. iam.PrincipalRepository provides it. It returns iam.ErrNotFound
// for a principal that is not one of the Organization's.
type Principals interface {
	Get(ctx context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error)
}

// Agents stores the Agents a Team creates. iam.AgentRepository provides it.
// It returns iam.ErrAlreadyExists for a slug another of the Organization's
// Agents holds, and joins the transaction its context carries.
type Agents interface {
	Write(ctx context.Context, agent *iam.Agent) error
}

// TeamService manages an Organization's Teams and their members.
type TeamService interface {
	CreateTeam(ctx context.Context, actor identity.Actor, in CreateTeamInput) (*Team, error)
	GetTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*Team, error)
	GetTeamBySlug(ctx context.Context, actor identity.Actor, organizationID, slug string) (*Team, error)
	ListTeams(ctx context.Context, actor identity.Actor, query ListTeamsQuery) (*pagination.Result[*Team], error)
	UpdateTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string, props UpdateTeamProps) (*Team, error)
	ArchiveTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*Team, error)
	RestoreTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*Team, error)

	// ListTeamMembers returns the Team's members, sorted by principal ID.
	ListTeamMembers(ctx context.Context, actor identity.Actor, organizationID, teamID string) ([]TeamMemberSnapshot, error)
	// SetTeamMember adds an active user or Agent to the Team with role, or
	// changes the role of a member.
	SetTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string, role Role) (*Team, error)
	// RemoveTeamMember removes a member. It refuses with ErrLastTeam to
	// remove an Agent from its only active Team.
	RemoveTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string) (*Team, error)

	// CreateAgent creates an Agent in IAM as a member of an active Team,
	// which is accountable for it.
	CreateAgent(ctx context.Context, actor identity.Actor, in CreateAgentInput) (*CreatedAgent, error)
}

// CreateAgentInput describes a new Agent and its first Team.
type CreateAgentInput struct {
	OrganizationID string
	TeamID         string
	// Slug is derived from DisplayName when empty. It is unique among the
	// Organization's Agents.
	Slug        string
	DisplayName string
	// Role is the Agent's role on the Team, RoleMember when empty.
	Role Role
}

// CreatedAgent is a new Agent and the Team it joined.
type CreatedAgent struct {
	Agent *iam.Agent
	Team  *Team
}

// CreateTeamInput describes a new Team. It starts with no members.
type CreateTeamInput struct {
	OrganizationID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description *string
}

// Owned is a kind of active record a Team owns in another capability, such
// as its Applications.
type Owned struct {
	Capability string
	// Kind names what is owned, such as "Application".
	Kind  string
	Count int
}

func (o Owned) String() string {
	return fmt.Sprintf("%d active %s (%s)", o.Count, o.Kind, o.Capability)
}

// Owner is implemented by every capability that records an owning Team. A
// Team cannot be archived while any reports active records it owns: they
// must be handed to another Team first, so nothing is left without an owner.
type Owner interface {
	ActiveOwned(ctx context.Context, organizationID, teamID string) ([]Owned, error)
}

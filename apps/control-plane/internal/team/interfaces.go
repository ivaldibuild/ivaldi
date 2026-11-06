package team

// The interfaces this capability defines for itself: its repository, the
// lookup it needs from IAM, and its service. Directory, the contract other
// capabilities depend on, is in directory.go.

import (
	"context"

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
}

// Users finds the users that may become members. iam.UserRepository
// provides it. It returns iam.ErrNotFound for a principal that is not a
// user in the Organization.
type Users interface {
	Get(ctx context.Context, organizationID, principalID string) (*iam.User, error)
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
	// SetTeamMember adds an active user to the Team with role, or changes
	// the role of a member.
	SetTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string, role Role) (*Team, error)
	RemoveTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string) (*Team, error)
}

// CreateTeamInput describes a new Team. It starts with no members.
type CreateTeamInput struct {
	OrganizationID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description *string
}

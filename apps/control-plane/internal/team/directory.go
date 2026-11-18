package team

import (
	"context"
	"errors"
	"time"
)

// Membership is one Team a principal belongs to, for capabilities that show
// an Agent's Teams without taking a dependency on the team service.
type Membership struct {
	TeamID      string     `json:"team_id"`
	Slug        string     `json:"slug"`
	DisplayName string     `json:"display_name"`
	Role        Role       `json:"role" enum:"lead,member"`
	ArchivedAt  *time.Time `json:"archived_at"`
}

// TeamName identifies a Team by its ID and display name, for
// capabilities that label their records with the Team without reading its
// members.
type TeamName struct {
	ID          string
	DisplayName string
}

// Directory answers questions about Teams. Capabilities that record an
// owning Team check it here before storing it. It does not authorize: the
// calling capability has already authorized its own operation.
type Directory interface {
	// IsActiveTeam reports whether teamID is an active Team in the Organization.
	IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error)
	// ListTeamsForMembers returns each principal's Team memberships, keyed by
	// principal ID, including archived Teams.
	ListTeamsForMembers(ctx context.Context, organizationID string, principalIDs []string) (map[string][]Membership, error)
	// LookupTeams returns the Teams with the IDs, archived ones included,
	// keyed by ID. IDs that match nothing are left out.
	LookupTeams(ctx context.Context, organizationID string, teamIDs []string) (map[string]TeamName, error)
}

// NewDirectory returns a Directory that reads through repo.
func NewDirectory(repo TeamRepository) Directory {
	return directory{repo: repo}
}

type directory struct {
	repo TeamRepository
}

func (d directory) IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error) {
	t, err := d.repo.GetTeam(ctx, organizationID, teamID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !t.Archived(), nil
}

func (d directory) ListTeamsForMembers(ctx context.Context, organizationID string, principalIDs []string) (map[string][]Membership, error) {
	return d.repo.ListTeamsForMembers(ctx, organizationID, principalIDs)
}

func (d directory) LookupTeams(ctx context.Context, organizationID string, teamIDs []string) (map[string]TeamName, error) {
	names, err := d.repo.GetTeamNames(ctx, organizationID, teamIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]TeamName, len(names))
	for _, name := range names {
		out[name.ID] = name
	}
	return out, nil
}

// MemberLookup lists the people in a Team, for capabilities that route work
// to it, such as notifications. It does not authorize.
type MemberLookup interface {
	// ListTeamUsers returns the IDs of the users in an active Team. An
	// archived or unknown Team has none.
	ListTeamUsers(ctx context.Context, organizationID, teamID string) ([]string, error)
}

// NewMemberLookup returns a MemberLookup that reads through repo.
func NewMemberLookup(repo TeamRepository) MemberLookup {
	return directory{repo: repo}
}

func (d directory) ListTeamUsers(ctx context.Context, organizationID, teamID string) ([]string, error) {
	t, err := d.repo.GetTeam(ctx, organizationID, teamID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t.Archived() {
		return nil, nil
	}
	var users []string
	for _, m := range t.Members() {
		if m.PrincipalKind == MemberUser {
			users = append(users, m.PrincipalID)
		}
	}
	return users, nil
}

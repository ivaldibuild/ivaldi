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

// Directory answers questions about Teams. Capabilities that record an
// owning Team check it here before storing it. It does not authorize: the
// calling capability has already authorized its own operation.
type Directory interface {
	// IsActiveTeam reports whether teamID is an active Team in the Organization.
	IsActiveTeam(ctx context.Context, organizationID, teamID string) (bool, error)
	// ListTeamsForMembers returns each principal's Team memberships, keyed by
	// principal ID, including archived Teams.
	ListTeamsForMembers(ctx context.Context, organizationID string, principalIDs []string) (map[string][]Membership, error)
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

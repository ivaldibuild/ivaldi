package team

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// GroupName names the IAM group that mirrors t. An archived Team's slug may
// be taken by an active one, and group names are unique in the
// Organization, so an archived Team's group is named by its ID instead.
func GroupName(t *Team) string {
	if t.Archived() {
		return "team:" + t.ID()
	}
	return "team:" + t.Slug()
}

// SyncGroup makes t's IAM group match it: named for it, with its members,
// or with none while it is archived, so its grants stop applying. It
// returns the group's ID.
func SyncGroup(ctx context.Context, groups iam.TeamGroups, t *Team) (string, error) {
	var members []string
	if !t.Archived() {
		for _, m := range t.Members() {
			members = append(members, m.PrincipalID)
		}
	}
	groupID, err := groups.SyncTeamGroup(ctx, t.OrganizationID(), t.ID(), GroupName(t), members)
	if err != nil {
		return "", fmt.Errorf("sync the group of team %s: %w", t.ID(), err)
	}
	return groupID, nil
}

// NewGroupSource returns the iam.TeamGroupSource IAM grants roles to Teams
// through. It locks the Team and syncs its group before returning the
// group's ID, so a Team whose group was never created gets one, and a
// membership change cannot interleave. It does not authorize: the grant
// that asks for it is authorized.
func NewGroupSource(teams TeamRepository, groups iam.TeamGroups) (iam.TeamGroupSource, error) {
	switch {
	case teams == nil:
		return nil, errors.New("team group source: TeamRepository is required")
	case groups == nil:
		return nil, errors.New("team group source: TeamGroups is required")
	}
	return &groupSource{teams: teams, groups: groups}, nil
}

type groupSource struct {
	teams  TeamRepository
	groups iam.TeamGroups
}

func (s *groupSource) TeamGroupID(ctx context.Context, organizationID, teamID string) (string, error) {
	t, err := s.teams.GetTeamForUpdate(ctx, organizationID, teamID)
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("team %s: %w", teamID, iam.ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if t.Archived() {
		return "", fmt.Errorf("team %s is archived: %w", teamID, iam.ErrStatus)
	}
	return SyncGroup(ctx, s.groups, t)
}

package team

import (
	"context"
	"errors"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// NewPrincipalTeamSource returns the iam.PrincipalTeamSource that names the
// active Teams principals belong to, reading memberships through d.
func NewPrincipalTeamSource(d Directory) (iam.PrincipalTeamSource, error) {
	if d == nil {
		return nil, errors.New("principal team source: Directory is required")
	}
	return principalTeams{directory: d}, nil
}

type principalTeams struct {
	directory Directory
}

func (s principalTeams) ListActiveTeams(ctx context.Context, organizationID string, principalIDs []string) (map[string][]iam.PrincipalTeam, error) {
	memberships, err := s.directory.ListTeamsForMembers(ctx, organizationID, principalIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]iam.PrincipalTeam, len(memberships))
	for principalID, ms := range memberships {
		for _, m := range ms {
			if m.ArchivedAt != nil {
				continue
			}
			out[principalID] = append(out[principalID], iam.PrincipalTeam{ID: m.TeamID, Slug: m.Slug, DisplayName: m.DisplayName})
		}
	}
	return out, nil
}

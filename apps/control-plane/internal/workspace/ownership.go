package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
)

// ProjectOwnership counts the active Projects a Team owns. Repository
// provides it.
type ProjectOwnership interface {
	CountActiveProjectsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error)
}

// NewTeamOwnership returns the team.Owner that reports a Team's active
// Projects, so a Team that owns any cannot be archived.
func NewTeamOwnership(projects ProjectOwnership) (team.Owner, error) {
	if projects == nil {
		return nil, errors.New("workspace team ownership: projects are required")
	}
	return teamOwnership{projects: projects}, nil
}

type teamOwnership struct {
	projects ProjectOwnership
}

func (o teamOwnership) ActiveOwned(ctx context.Context, organizationID, teamID string) ([]team.Owned, error) {
	n, err := o.projects.CountActiveProjectsOwnedBy(ctx, organizationID, teamID)
	if err != nil {
		return nil, fmt.Errorf("count projects owned by team %s: %w", teamID, err)
	}
	return []team.Owned{{Capability: "workspace", Kind: "Project", Count: n}}, nil
}

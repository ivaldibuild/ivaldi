package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
)

// ApplicationOwnership counts the active Applications a Team owns.
// ApplicationRepository provides it.
type ApplicationOwnership interface {
	CountActiveApplicationsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error)
}

// NewTeamOwnership returns the team.Owner that reports a Team's active
// Applications, so a Team that owns any cannot be archived until they are
// handed over.
func NewTeamOwnership(applications ApplicationOwnership) (team.Owner, error) {
	if applications == nil {
		return nil, errors.New("delivery team ownership: applications are required")
	}
	return teamOwnership{applications: applications}, nil
}

type teamOwnership struct {
	applications ApplicationOwnership
}

func (o teamOwnership) ActiveOwned(ctx context.Context, organizationID, teamID string) ([]team.Owned, error) {
	n, err := o.applications.CountActiveApplicationsOwnedBy(ctx, organizationID, teamID)
	if err != nil {
		return nil, fmt.Errorf("count applications owned by team %s: %w", teamID, err)
	}
	return []team.Owned{{Capability: "delivery", Kind: "Application", Count: n}}, nil
}

package team

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// teamsByID is a TeamRepository that only gets Teams, failing for broken.
type teamsByID struct {
	TeamRepository
	teams  map[string]*Team
	broken string
}

func (r teamsByID) GetTeam(_ context.Context, organizationID, teamID string) (*Team, error) {
	if teamID == r.broken {
		return nil, errors.New("connection reset")
	}
	t, ok := r.teams[teamID]
	if !ok || t.OrganizationID() != organizationID {
		return nil, ErrNotFound
	}
	return t, nil
}

func (teamsByID) ListTeams(context.Context, ListTeamsQuery) (*pagination.Result[*Team], error) {
	return nil, errors.New("not used")
}

func TestDirectory(t *testing.T) {
	ctx := context.Background()
	active := newTestTeam(t)
	archived := newTestTeam(t)
	archived.Archive()
	d := NewDirectory(teamsByID{
		teams:  map[string]*Team{active.ID(): active, archived.ID(): archived},
		broken: "tem_broken",
	})

	for name, tc := range map[string]struct {
		org, teamID string
		want        bool
	}{
		"an active team":                 {org, active.ID(), true},
		"an archived team":               {org, archived.ID(), false},
		"a missing team":                 {org, "tem_01ARZ3NDEKTSV4RRFFQ69G5FAV", false},
		"a team of another organization": {"org_other", active.ID(), false},
	} {
		t.Run(name, func(t *testing.T) {
			ok, err := d.IsActiveTeam(ctx, tc.org, tc.teamID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ok)
		})
	}

	_, err := d.IsActiveTeam(ctx, org, "tem_broken")
	assert.Error(t, err, "other errors are returned")
}

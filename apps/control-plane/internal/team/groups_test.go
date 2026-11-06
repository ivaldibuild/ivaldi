package team

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// lockingTeams returns its Teams and remembers the ones it locked. Calling
// anything but GetTeamForUpdate panics.
type lockingTeams struct {
	TeamRepository
	teams  map[string]*Team
	locked []string
}

func (r *lockingTeams) GetTeamForUpdate(_ context.Context, _, teamID string) (*Team, error) {
	r.locked = append(r.locked, teamID)
	t, ok := r.teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

type syncedGroup struct {
	name    string
	members []string
}

type recordingGroups map[string]syncedGroup

func (g recordingGroups) SyncTeamGroup(_ context.Context, _, teamID, name string, members []string) (string, error) {
	g[teamID] = syncedGroup{name: name, members: members}
	return "grp_" + teamID, nil
}

func TestGroupSource(t *testing.T) {
	ctx := context.Background()
	platform, err := NewTeam(teamProps(), nil)
	require.NoError(t, err)
	require.NoError(t, platform.SetMember("prn_01JZ00000000000000000000AD", MemberUser, RoleLead))
	old, err := NewTeam(NewTeamProps{OrganizationID: platform.OrganizationID(), DisplayName: "Old"}, nil)
	require.NoError(t, err)
	old.Archive()

	teams := &lockingTeams{teams: map[string]*Team{platform.ID(): platform, old.ID(): old}}
	groups := recordingGroups{}
	source, err := NewGroupSource(teams, groups)
	require.NoError(t, err)

	t.Run("syncs the Team's group and returns it", func(t *testing.T) {
		groupID, err := source.TeamGroupID(ctx, platform.OrganizationID(), platform.ID())
		require.NoError(t, err)
		assert.Equal(t, "grp_"+platform.ID(), groupID)
		assert.Equal(t, syncedGroup{name: "team:" + platform.Slug(), members: []string{"prn_01JZ00000000000000000000AD"}}, groups[platform.ID()])
		assert.Equal(t, []string{platform.ID()}, teams.locked, "the Team is locked while its group syncs")
	})

	t.Run("an archived Team cannot be granted roles", func(t *testing.T) {
		_, err := source.TeamGroupID(ctx, old.OrganizationID(), old.ID())
		assert.ErrorIs(t, err, iam.ErrStatus)
	})

	t.Run("an unknown Team is IAM's not found", func(t *testing.T) {
		_, err := source.TeamGroupID(ctx, platform.OrganizationID(), "tem_unknown")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("needs its dependencies", func(t *testing.T) {
		_, err := NewGroupSource(nil, groups)
		assert.Error(t, err)
		_, err = NewGroupSource(teams, nil)
		assert.Error(t, err)
	})
}

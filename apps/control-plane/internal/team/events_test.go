package team

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestNewRecordsCreatedOnlyForNewTeams(t *testing.T) {
	tm := newTestTeam(t)
	assert.Equal(t, []aggregate.Event{TeamCreated{tm.Snapshot()}}, tm.PullEvents())

	stored, err := NewTeam(teamProps(), new(id.New("tem")))
	require.NoError(t, err)
	assert.Empty(t, stored.PullEvents())
}

func TestTeamEvents(t *testing.T) {
	// withAlice is a Team whose events so far are pulled, with Alice as a member.
	withAlice := func(t *testing.T, tm *Team) {
		require.NoError(t, tm.SetMember(alice, RoleMember))
		tm.PullEvents()
	}
	cases := []struct {
		name   string
		change func(t *testing.T, tm *Team)
		want   func(tm *Team) aggregate.Event
	}{
		{"update", func(t *testing.T, tm *Team) {
			require.NoError(t, tm.Update(UpdateTeamProps{DisplayName: new("Core platform")}))
		}, func(tm *Team) aggregate.Event { return TeamUpdated{tm.Snapshot()} }},
		{"update with the same values", func(t *testing.T, tm *Team) {
			require.NoError(t, tm.Update(UpdateTeamProps{Slug: new("platform"), DisplayName: new("Platform"), Description: new("Runs the platform")}))
		}, nil},
		{"add a member", func(t *testing.T, tm *Team) { require.NoError(t, tm.SetMember(alice, RoleLead)) },
			func(tm *Team) aggregate.Event { return TeamMemberAdded{Team: tm.Snapshot(), PrincipalID: alice} }},
		{"change a member's role", func(t *testing.T, tm *Team) {
			withAlice(t, tm)
			require.NoError(t, tm.SetMember(alice, RoleLead))
		}, func(tm *Team) aggregate.Event {
			return TeamMemberRoleChanged{Team: tm.Snapshot(), PrincipalID: alice, PreviousRole: RoleMember}
		}},
		{"set a member's current role", func(t *testing.T, tm *Team) {
			withAlice(t, tm)
			require.NoError(t, tm.SetMember(alice, RoleMember))
		}, nil},
		{"remove a member", func(t *testing.T, tm *Team) {
			withAlice(t, tm)
			require.NoError(t, tm.RemoveMember(alice))
		}, func(tm *Team) aggregate.Event {
			return TeamMemberRemoved{Team: tm.Snapshot(), PrincipalID: alice, PreviousRole: RoleMember}
		}},
		{"remove someone who is not a member", func(t *testing.T, tm *Team) {
			require.ErrorIs(t, tm.RemoveMember(alice), ErrNotFound)
		}, nil},
		{"archive", func(t *testing.T, tm *Team) { tm.Archive() },
			func(tm *Team) aggregate.Event { return TeamArchived{tm.Snapshot()} }},
		{"archive twice", func(t *testing.T, tm *Team) { tm.Archive(); tm.PullEvents(); tm.Archive() }, nil},
		{"restore", func(t *testing.T, tm *Team) { tm.Archive(); tm.PullEvents(); tm.Restore() },
			func(tm *Team) aggregate.Event { return TeamRestored{tm.Snapshot()} }},
		{"restore an active one", func(t *testing.T, tm *Team) { tm.Restore() }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tm := newTestTeam(t)
			tm.PullEvents()

			c.change(t, tm)

			got := tm.PullEvents()
			if c.want == nil {
				assert.Empty(t, got, "nothing changed")
				return
			}
			assert.Equal(t, []aggregate.Event{c.want(tm)}, got)
		})
	}
}

func TestEventNames(t *testing.T) {
	for _, e := range []aggregate.Event{
		TeamCreated{}, TeamUpdated{}, TeamMemberAdded{}, TeamMemberRoleChanged{}, TeamMemberRemoved{}, TeamArchived{}, TeamRestored{},
	} {
		assert.Regexp(t, `^team\.v1\.team\.[a-z_]+$`, e.EventName(), "%T", e)
	}
}

func TestSnapshotJSONKeys(t *testing.T) {
	timestamps := []string{"created_at", "updated_at"}
	tests := []struct {
		snapshot any
		keys     []string
	}{
		{TeamSnapshot{}, append([]string{"id", "organization_id", "slug", "display_name", "description", "members", "archived_at"}, timestamps...)},
		{TeamMemberSnapshot{}, append([]string{"principal_id", "role"}, timestamps...)},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.snapshot)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &fields))

		assert.ElementsMatch(t, tt.keys, slices.Collect(maps.Keys(fields)), "%T", tt.snapshot)
	}
}

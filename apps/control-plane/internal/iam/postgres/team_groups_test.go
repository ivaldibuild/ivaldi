package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

type teamGroupRow struct {
	ID         string
	Name       string
	Source     string
	ExternalID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (f *fixture) teamGroup(t *testing.T, orgID, teamID string) (teamGroupRow, bool) {
	t.Helper()
	var rows []teamGroupRow
	require.NoError(t, f.db.Raw(
		"SELECT id, name, source, external_id, created_at, updated_at FROM iam.groups WHERE organization_id = ? AND external_id = ?",
		orgID, teamID).Scan(&rows).Error)
	require.LessOrEqual(t, len(rows), 1, "a team has one group")
	if len(rows) == 0 {
		return teamGroupRow{}, false
	}
	return rows[0], true
}

type groupMemberRow struct {
	PrincipalID string
	CreatedAt   time.Time
}

func (f *fixture) groupMembers(t *testing.T, groupID string) []groupMemberRow {
	t.Helper()
	var rows []groupMemberRow
	require.NoError(t, f.db.Raw("SELECT principal_id, created_at FROM iam.group_members WHERE group_id = ? ORDER BY principal_id", groupID).Scan(&rows).Error)
	return rows
}

func memberIDs(rows []groupMemberRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.PrincipalID)
	}
	return out
}

func TestSyncTeamGroup(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	groups := NewTeamGroups(f.db)
	agents := NewAgentRepository(f.db)
	org := f.writeOrg(t, "acme")
	other := f.writeOrg(t, "other")

	ada := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, f.users.Write(ctx, ada))
	bot := newAgent(org.ID(), "release-agent")
	require.NoError(t, agents.Write(ctx, bot))
	grace := newUser(org.ID(), f.writeAccount(t, "grace@example.com").ID())
	require.NoError(t, f.users.Write(ctx, grace))
	teamID := "tem_" + id.ULID()

	t.Run("creates the group with its members", func(t *testing.T) {
		groupID, err := groups.SyncTeamGroup(ctx, org.ID(), teamID, "team:platform", []string{ada.ID(), bot.ID(), ada.ID()})
		require.NoError(t, err)

		g, ok := f.teamGroup(t, org.ID(), teamID)
		require.True(t, ok)
		assert.Equal(t, g.ID, groupID, "returns the group's ID")
		assert.Equal(t, "team:platform", g.Name)
		assert.Equal(t, string(iam.GroupTeam), g.Source)
		assert.Equal(t, teamID, g.ExternalID)
		assert.ElementsMatch(t, []string{ada.ID(), bot.ID()}, memberIDs(f.groupMembers(t, g.ID)), "duplicates count once")
	})

	t.Run("is idempotent", func(t *testing.T) {
		before, _ := f.teamGroup(t, org.ID(), teamID)
		members := f.groupMembers(t, before.ID)
		at(t, time.Hour)

		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), teamID, "team:platform", []string{bot.ID(), ada.ID()})))

		after, _ := f.teamGroup(t, org.ID(), teamID)
		assert.Equal(t, before.UpdatedAt, after.UpdatedAt, "nothing changed")
		assert.Equal(t, members, f.groupMembers(t, after.ID))
	})

	t.Run("renames the group and replaces its members", func(t *testing.T) {
		before, _ := f.teamGroup(t, org.ID(), teamID)
		joined := f.groupMembers(t, before.ID)
		at(t, 2*time.Hour)

		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), teamID, "team:core", []string{bot.ID(), grace.ID()})))

		g, _ := f.teamGroup(t, org.ID(), teamID)
		assert.Equal(t, before.ID, g.ID, "the same group")
		assert.Equal(t, "team:core", g.Name)
		assert.Equal(t, t0.Add(2*time.Hour), g.UpdatedAt.UTC())
		members := f.groupMembers(t, g.ID)
		assert.ElementsMatch(t, []string{bot.ID(), grace.ID()}, memberIDs(members))
		for _, m := range members {
			if m.PrincipalID == bot.ID() {
				for _, j := range joined {
					if j.PrincipalID == bot.ID() {
						assert.Equal(t, j.CreatedAt, m.CreatedAt, "a kept member keeps when it joined")
					}
				}
			}
		}
	})

	t.Run("no members empties the group", func(t *testing.T) {
		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), teamID, "team:core", nil)))
		g, _ := f.teamGroup(t, org.ID(), teamID)
		assert.Empty(t, f.groupMembers(t, g.ID))
	})

	t.Run("another organization's team group is separate", func(t *testing.T) {
		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, other.ID(), teamID, "team:core", nil)))
		_, ok := f.teamGroup(t, other.ID(), teamID)
		assert.True(t, ok)
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.groups WHERE organization_id = ?", org.ID()))
	})

	t.Run("a member from another organization is refused", func(t *testing.T) {
		_, err := groups.SyncTeamGroup(ctx, other.ID(), teamID, "team:core", []string{ada.ID()})
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("a name another group holds is refused", func(t *testing.T) {
		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), "tem_"+id.ULID(), "team:taken", nil)))
		_, err := groups.SyncTeamGroup(ctx, org.ID(), "tem_"+id.ULID(), "team:taken", nil)
		assert.ErrorIs(t, err, iam.ErrAlreadyExists)
	})

	t.Run("manual groups are untouched", func(t *testing.T) {
		manual := id.New("grp")
		require.NoError(t, f.db.Exec(`INSERT INTO iam.groups (id, organization_id, name, source, external_id) VALUES (?, ?, 'Manual', 'manual', NULL)`, manual, org.ID()).Error)
		require.NoError(t, f.db.Exec(`INSERT INTO iam.group_members (organization_id, group_id, principal_id) VALUES (?, ?, ?)`, org.ID(), manual, ada.ID()).Error)

		require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), teamID, "team:core", []string{grace.ID()})))

		assert.Equal(t, []string{ada.ID()}, memberIDs(f.groupMembers(t, manual)))
	})

	t.Run("joins the context transaction", func(t *testing.T) {
		abort := errors.New("abort")
		rolledBack := "tem_" + id.ULID()
		err := database.NewTransactor(f.db).WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, syncErr(groups.SyncTeamGroup(ctx, org.ID(), rolledBack, "team:rolled-back", []string{ada.ID()})))
			return abort
		})
		require.ErrorIs(t, err, abort)
		_, ok := f.teamGroup(t, org.ID(), rolledBack)
		assert.False(t, ok)
	})
}

// A team group's grants reach its members, as any group's do.
func TestTeamGroupGrantsReachMembers(t *testing.T) {
	w := newAccessWorld(t)
	teamID := "tem_" + id.ULID()
	require.NoError(t, syncErr(NewTeamGroups(w.db).SyncTeamGroup(w.ctx, w.org, teamID, "team:platform", []string{w.ada})))
	g, ok := w.teamGroup(t, w.org, teamID)
	require.True(t, ok)
	w.grant(t, iam.RoleViewer, w.ws, nil, &g.ID, nil)

	assert.Equal(t, []string{roleID(iam.RoleViewer)}, w.check(t, w.ws).RoleIDs)

	require.NoError(t, syncErr(NewTeamGroups(w.db).SyncTeamGroup(w.ctx, w.org, teamID, "team:platform", nil)))
	assert.Empty(t, w.check(t, w.ws).RoleIDs, "a team group without members passes on nothing")
}

// syncErr keeps SyncTeamGroup's error, for checks that do not need the ID.
func syncErr(_ string, err error) error { return err }

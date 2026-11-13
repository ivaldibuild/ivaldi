package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

func TestTeamRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		agent := id.New("prn")
		tm := must(team.NewTeam(team.NewTeamProps{
			OrganizationID: orgA,
			Slug:           "platform",
			DisplayName:    "Platform",
			Description:    new("Runs the platform"),
			Members:        []team.NewTeamMemberProps{member(bob, team.RoleMember), member(alice, team.RoleLead), agentMember(agent, team.RoleMember)},
		}, nil))
		require.NoError(t, f.teams.CreateTeam(ctx, tm))

		got, err := f.teams.GetTeam(ctx, orgA, tm.ID())
		require.NoError(t, err)
		assert.Equal(t, tm.Snapshot(), got.Snapshot())
		assert.Len(t, got.Members(), 3)
		m, _ := got.Member(agent)
		assert.Equal(t, team.MemberAgent, m.PrincipalKind, "the member's kind round trips")
		assert.Empty(t, got.PullEvents(), "a loaded team records no event")

		bySlug, err := f.teams.GetTeamBySlug(ctx, orgA, "platform")
		require.NoError(t, err)
		assert.Equal(t, tm.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip without members or description", func(t *testing.T) {
		tm := f.createTeam(t, orgA, "empty")
		got, err := f.teams.GetTeam(ctx, orgA, tm.ID())
		require.NoError(t, err)
		assert.Equal(t, tm.Snapshot(), got.Snapshot())
		assert.Empty(t, got.Members())
		assert.Nil(t, got.Description())
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		tm := f.createTeam(t, orgA, "isolated", member(alice, team.RoleLead))
		_, err := f.teams.GetTeam(ctx, orgB, tm.ID())
		assert.ErrorIs(t, err, team.ErrNotFound)
		_, err = f.teams.GetTeamForUpdate(ctx, orgB, tm.ID())
		assert.ErrorIs(t, err, team.ErrNotFound)
		_, err = f.teams.GetTeamBySlug(ctx, orgB, "isolated")
		assert.ErrorIs(t, err, team.ErrNotFound)

		page, err := f.teams.ListTeams(ctx, team.ListTeamsQuery{OrganizationID: orgB})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		assert.Zero(t, page.TotalCount)

		other := must(team.NewTeam(team.NewTeamProps{OrganizationID: orgB, Slug: "isolated", DisplayName: "Isolated"}, new(tm.ID())))
		assert.ErrorIs(t, f.teams.UpdateTeam(ctx, other), team.ErrNotFound, "an update names the organization too")
	})

	t.Run("slugs are unique among an organization's active teams", func(t *testing.T) {
		first := f.createTeam(t, orgA, "clash")
		assert.ErrorIs(t, f.teams.CreateTeam(ctx, newTeam(orgA, "clash")), team.ErrSlugTaken)
		f.createTeam(t, orgB, "clash")

		first.Archive()
		require.NoError(t, f.teams.UpdateTeam(ctx, first))
		_, err := f.teams.GetTeamBySlug(ctx, orgA, "clash")
		assert.ErrorIs(t, err, team.ErrNotFound, "archived teams are not found by slug")
		second := f.createTeam(t, orgA, "clash")

		got, err := f.teams.GetTeamBySlug(ctx, orgA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.teams.UpdateTeam(ctx, first), team.ErrSlugTaken)
	})

	t.Run("a team ID twice conflicts", func(t *testing.T) {
		tm := f.createTeam(t, orgA, "twice")
		dup := must(team.NewTeam(team.NewTeamProps{OrganizationID: orgA, Slug: "twice-2", DisplayName: "Twice"}, new(tm.ID())))
		assert.ErrorIs(t, f.teams.CreateTeam(ctx, dup), team.ErrConflict)
	})

	t.Run("lists a principal's teams with roles", func(t *testing.T) {
		agent := id.New("prn")
		platform := f.createTeam(t, orgA, "platform-memberships", agentMember(agent, team.RoleLead))
		other := f.createTeam(t, orgA, "other-memberships", agentMember(agent, team.RoleMember))
		f.createTeam(t, orgA, "not-a-member")

		got, err := f.teams.ListTeamsForMembers(ctx, orgA, []string{agent})
		require.NoError(t, err)
		require.Len(t, got[agent], 2)
		assert.Equal(t, other.ID(), got[agent][0].TeamID)
		assert.Equal(t, team.RoleMember, got[agent][0].Role)
		assert.Equal(t, platform.ID(), got[agent][1].TeamID)
		assert.Equal(t, team.RoleLead, got[agent][1].Role)
	})

	t.Run("gets names without reading members", func(t *testing.T) {
		one := f.createTeam(t, orgA, "names-one", member(alice, team.RoleLead))
		two := f.createTeam(t, orgA, "names-two")
		names, err := f.teams.GetTeamNames(ctx, orgA, []string{one.ID(), two.ID(), "tem_missing"})
		require.NoError(t, err)
		byID := map[string]team.TeamName{}
		for _, name := range names {
			byID[name.ID] = name
		}
		assert.Equal(t, "names-one", byID[one.ID()].Slug)
		assert.Equal(t, "Team names-two", byID[two.ID()].DisplayName)
		assert.NotContains(t, byID, "tem_missing")

		names, err = f.teams.GetTeamNames(ctx, orgB, []string{one.ID()})
		require.NoError(t, err)
		assert.Empty(t, names, "another organization sees nothing")
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.teams.UpdateTeam(ctx, newTeam(orgA, "never-stored", member(carol, team.RoleLead))), team.ErrNotFound)
		assert.Zero(t, f.count(t, "SELECT count(*) FROM team.team_members WHERE principal_id = ?", carol))
	})
}

func TestTeamUpdateMembers(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	tm := f.createTeam(t, orgA, "platform", member(alice, team.RoleLead), member(bob, team.RoleMember))
	other := f.createTeam(t, orgA, "other", member(alice, team.RoleMember))
	joined := tm.Members()[0].CreatedAt

	at(t, time.Hour)
	require.NoError(t, tm.Update(team.UpdateTeamProps{DisplayName: new("Core Platform"), Description: new("Changed")}))
	require.NoError(t, tm.SetMember(alice, team.MemberUser, team.RoleMember))
	require.NoError(t, tm.RemoveMember(bob))
	require.NoError(t, tm.SetMember(carol, team.MemberUser, team.RoleLead))
	require.NoError(t, f.teams.UpdateTeam(ctx, tm))

	got, err := f.teams.GetTeam(ctx, orgA, tm.ID())
	require.NoError(t, err)
	assert.Equal(t, tm.Snapshot(), got.Snapshot())
	members := got.Members()
	require.Len(t, members, 2)
	assert.Equal(t, joined, members[0].CreatedAt, "a kept member keeps when they joined")
	assert.Equal(t, t0.Add(time.Hour), members[0].UpdatedAt)
	assert.Equal(t, team.RoleMember, members[0].Role)
	assert.Zero(t, f.count(t, "SELECT count(*) FROM team.team_members WHERE team_id = ? AND principal_id = ?", tm.ID(), bob), "the removed member is deleted")

	otherGot, err := f.teams.GetTeam(ctx, orgA, other.ID())
	require.NoError(t, err)
	assert.Equal(t, other.Snapshot(), otherGot.Snapshot(), "other teams' members are untouched")

	t.Run("removing every member", func(t *testing.T) {
		require.NoError(t, tm.RemoveMember(alice))
		require.NoError(t, tm.RemoveMember(carol))
		require.NoError(t, f.teams.UpdateTeam(ctx, tm))
		assert.Zero(t, f.count(t, "SELECT count(*) FROM team.team_members WHERE team_id = ?", tm.ID()))
	})
}

func TestTeamForUpdate(t *testing.T) {
	f := newFixture(t)
	tm := f.createTeam(t, orgA, "locked")

	t.Run("locks the row and commits", func(t *testing.T) {
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.teams.GetTeamForUpdate(ctx, orgA, tm.ID())
			require.NoError(t, err)
			assert.Equal(t, tm.Snapshot(), got.Snapshot())
			f.assertLocked(t, tm.ID())

			require.NoError(t, got.SetMember(alice, team.MemberUser, team.RoleLead))
			return f.teams.UpdateTeam(ctx, got)
		})
		require.NoError(t, err)
		f.assertNotLocked(t, tm.ID())

		got, err := f.teams.GetTeam(f.ctx, orgA, tm.ID())
		require.NoError(t, err)
		assert.Len(t, got.Members(), 1)
		tm = got
	})

	t.Run("a plain get does not lock", func(t *testing.T) {
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			_, err := f.teams.GetTeam(ctx, orgA, tm.ID())
			require.NoError(t, err)
			f.assertNotLocked(t, tm.ID())
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("a rollback discards the update", func(t *testing.T) {
		stop := errors.New("stop")
		err := f.tx.WithinTx(f.ctx, func(ctx context.Context) error {
			got, err := f.teams.GetTeamForUpdate(ctx, orgA, tm.ID())
			require.NoError(t, err)
			require.NoError(t, got.SetMember(bob, team.MemberUser, team.RoleMember))
			require.NoError(t, f.teams.UpdateTeam(ctx, got))
			return stop
		})
		require.ErrorIs(t, err, stop)

		got, err := f.teams.GetTeam(f.ctx, orgA, tm.ID())
		require.NoError(t, err)
		assert.Equal(t, tm.Snapshot(), got.Snapshot())
	})
}

func TestTeamPagination(t *testing.T) {
	f := newFixture(t)
	slugs := []string{"delta", "alpha", "golf", "charlie", "echo"}
	var created []string
	for i, s := range slugs {
		at(t, time.Duration(i)*time.Second)
		created = append(created, f.createTeam(t, orgA, s, member(alice, team.RoleLead)).ID())
	}
	at(t, time.Minute)
	archived := f.createTeam(t, orgA, "archived")
	archived.Archive()
	require.NoError(t, f.teams.UpdateTeam(f.ctx, archived))
	f.createTeam(t, orgB, "other-org")

	list := func(q team.ListTeamsQuery) *pagination.Result[*team.Team] {
		q.OrganizationID = orgA
		res, err := f.teams.ListTeams(f.ctx, q)
		require.NoError(t, err)
		return res
	}

	res := list(team.ListTeamsQuery{})
	assert.Equal(t, created, idsOf(res.Items), "by creation time by default, without archived teams")
	assert.Equal(t, 5, res.TotalCount)
	for _, tm := range res.Items {
		assert.Len(t, tm.Members(), 1, "teams come with their members")
	}

	res = list(team.ListTeamsQuery{IncludeArchived: true})
	assert.Equal(t, append(created, archived.ID()), idsOf(res.Items))

	first := list(team.ListTeamsQuery{Page: pagination.Query{Limit: 2, SortBy: "slug"}})
	require.True(t, first.HasNext)
	require.NotNil(t, first.NextCursor)
	second := list(team.ListTeamsQuery{Page: pagination.Query{Limit: 2, SortBy: "slug", NextCursor: first.NextCursor}})
	third := list(team.ListTeamsQuery{Page: pagination.Query{Limit: 2, SortBy: "slug", NextCursor: second.NextCursor}})
	var bySlug []string
	for _, p := range []*pagination.Result[*team.Team]{first, second, third} {
		for _, tm := range p.Items {
			bySlug = append(bySlug, tm.Slug())
		}
	}
	assert.Equal(t, []string{"alpha", "charlie", "delta", "echo", "golf"}, bySlug)
	assert.False(t, third.HasNext)

	_, err := f.teams.ListTeams(f.ctx, team.ListTeamsQuery{OrganizationID: orgA, Page: pagination.Query{SortBy: "members"}})
	assert.ErrorIs(t, err, team.ErrInvalid, "an unknown sort field is invalid input")
}

func TestCountActiveTeamsWithMember(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	f.createTeam(t, orgA, "one", agentMember(carol, team.RoleMember))
	two := f.createTeam(t, orgA, "two", agentMember(carol, team.RoleLead), member(alice, team.RoleLead))
	f.createTeam(t, orgA, "three", member(alice, team.RoleLead))
	f.createTeam(t, orgB, "other-org", agentMember(carol, team.RoleMember))

	count := func(org, principalID string) int {
		n, err := f.teams.CountActiveTeamsWithMember(ctx, org, principalID)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, 2, count(orgA, carol), "teams in another organization do not count")
	assert.Equal(t, 2, count(orgA, alice))
	assert.Equal(t, 0, count(orgA, bob))

	two.Archive()
	require.NoError(t, f.teams.UpdateTeam(ctx, two))
	assert.Equal(t, 1, count(orgA, carol), "archived teams do not count")
	assert.Equal(t, 1, count(orgB, carol))
}

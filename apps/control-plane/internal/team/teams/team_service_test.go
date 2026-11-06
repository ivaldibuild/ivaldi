package teams

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

func TestNewTeamServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]TeamServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewTeamService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewTeamService(all...)
	assert.NoError(t, err)
}

func TestCreateTeam(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	tm, err := w.svc.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "Release Engineering", Description: new("Ships things")})
	require.NoError(t, err)

	assert.Equal(t, "release-engineering", tm.Slug(), "slug derived from the display name")
	assert.Empty(t, tm.Members(), "a new team has no members")
	require.Len(t, w.authz.asked, 1)
	req := w.authz.asked[0]
	assert.Equal(t, team.PermissionTeamManage, req.Permission)
	assert.Equal(t, team.ActionTeamCreate, req.Action)
	assert.Equal(t, org, req.ResourceID, "authorized on the Organization")

	assert.Contains(t, w.teams, tm.ID())
	assert.Equal(t, []string{"team.v1.team.created"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, team.EventTeamCreated, w.audit[0].Type)
	assert.Equal(t, tm.ID(), w.audit[0].ResourceID)
	assert.Equal(t, "dec-"+team.ActionTeamCreate, w.audit[0].DecisionID)
	assert.Equal(t, ada, w.audit[0].Actor)
	assert.Equal(t, tm.Snapshot(), w.audit[0].After)
}

func TestCreateTeamRefusesATakenSlug(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.team(t, "platform", false)
	before := w.state.clone()

	_, err := w.svc.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "Platform"})

	require.ErrorIs(t, err, team.ErrSlugTaken)
	assert.Equal(t, before, w.state)
}

// Every operation asks the authorizer the right question, on the
// Organization, before it does anything.
func TestOperationsAreAuthorizedOnTheOrganization(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tm := w.team(t, "platform", false, lead(alice))
	archived := w.team(t, "old", true)

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
	}{
		{"create", func() error {
			_, err := w.svc.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "New"})
			return err
		}, team.PermissionTeamManage, team.ActionTeamCreate},
		{"get", func() error {
			_, err := w.svc.GetTeam(ctx, ada, org, tm.ID())
			return err
		}, team.PermissionTeamRead, team.ActionTeamRead},
		{"get by slug", func() error {
			_, err := w.svc.GetTeamBySlug(ctx, ada, org, "platform")
			return err
		}, team.PermissionTeamRead, team.ActionTeamRead},
		{"list", func() error {
			_, err := w.svc.ListTeams(ctx, ada, team.ListTeamsQuery{OrganizationID: org})
			return err
		}, team.PermissionTeamRead, team.ActionTeamList},
		{"update", func() error {
			_, err := w.svc.UpdateTeam(ctx, ada, org, tm.ID(), team.UpdateTeamProps{DisplayName: new("Platform 2")})
			return err
		}, team.PermissionTeamManage, team.ActionTeamUpdate},
		{"archive", func() error {
			_, err := w.svc.ArchiveTeam(ctx, ada, org, archived.ID())
			return err
		}, team.PermissionTeamManage, team.ActionTeamArchive},
		{"restore", func() error {
			_, err := w.svc.RestoreTeam(ctx, ada, org, tm.ID())
			return err
		}, team.PermissionTeamManage, team.ActionTeamRestore},
		{"list members", func() error {
			_, err := w.svc.ListTeamMembers(ctx, ada, org, tm.ID())
			return err
		}, team.PermissionTeamRead, team.ActionTeamListMembers},
		{"set member", func() error {
			_, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), alice, team.RoleLead)
			return err
		}, team.PermissionTeamManage, team.ActionTeamSetMember},
		{"remove member", func() error {
			_, err := w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), alice)
			return err
		}, team.PermissionTeamManage, team.ActionTeamRemoveMember},
		{"create agent", func() error {
			_, err := w.svc.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: tm.ID(), DisplayName: "Release agent"})
			return err
		}, team.PermissionAgentManage, team.ActionTeamCreateAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.reset()
			require.NoError(t, tc.call())
			require.Len(t, w.authz.asked, 1)
			req := w.authz.asked[0]
			assert.Equal(t, ada, req.Actor)
			assert.Equal(t, org, req.OrganizationID)
			assert.Equal(t, org, req.ResourceID)
			assert.Equal(t, tc.permission, req.Permission)
			assert.Equal(t, tc.action, req.Action)
		})
	}
}

// A refused operation writes nothing, and a refused read returns nothing.
func TestRefusalsWriteNothing(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(w *world, tm *team.Team) error{
		"create": func(w *world, _ *team.Team) error {
			_, err := w.svc.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "New"})
			return err
		},
		"get": func(w *world, tm *team.Team) error {
			_, err := w.svc.GetTeam(ctx, ada, org, tm.ID())
			return err
		},
		"update": func(w *world, tm *team.Team) error {
			_, err := w.svc.UpdateTeam(ctx, ada, org, tm.ID(), team.UpdateTeamProps{DisplayName: new("Renamed")})
			return err
		},
		"archive": func(w *world, tm *team.Team) error {
			_, err := w.svc.ArchiveTeam(ctx, ada, org, tm.ID())
			return err
		},
		"set member": func(w *world, tm *team.Team) error {
			_, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleMember)
			return err
		},
		"remove member": func(w *world, tm *team.Team) error {
			_, err := w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), alice)
			return err
		},
		"create agent": func(w *world, tm *team.Team) error {
			_, err := w.svc.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: tm.ID(), DisplayName: "Release agent"})
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tm := w.team(t, "platform", false, lead(alice))
			before := w.state.clone()
			w.authz.deny = func(authorization.Request) bool { return true }

			err := call(w, tm)

			require.ErrorIs(t, err, authorization.ErrDenied)
			assert.Equal(t, before, w.state)
		})
	}
}

// The service under the wrapper refuses to change anything without a
// decision, so wiring it up without authorization fails loudly.
func TestServiceWithoutAuthorizationChangesNothing(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tm := w.team(t, "platform", false, lead(alice))
	core := w.svc.(*authorized).next
	before := w.state.clone()

	_, err := core.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "New"})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateTeam(ctx, ada, org, tm.ID(), team.UpdateTeamProps{DisplayName: new("Renamed")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveTeam(ctx, ada, org, tm.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleMember)
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.RemoveTeamMember(ctx, ada, org, tm.ID(), alice)
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: tm.ID(), DisplayName: "Release agent"})
	assert.ErrorIs(t, err, change.ErrUnauthorized)

	assert.Equal(t, before, w.state)
}

func TestUpdateTeam(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tm := w.team(t, "platform", false)

	got, err := w.svc.UpdateTeam(ctx, ada, org, tm.ID(), team.UpdateTeamProps{DisplayName: new("Core Platform"), Slug: new("core")})
	require.NoError(t, err)

	assert.Equal(t, "Core Platform", got.DisplayName())
	assert.Equal(t, "core", w.teams[tm.ID()].Slug)
	assert.Equal(t, []string{"team.v1.team.updated"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, tm.Snapshot(), w.audit[0].Before)
	assert.Equal(t, got.Snapshot(), w.audit[0].After)
	assert.Equal(t, "dec-"+team.ActionTeamUpdate, w.audit[0].DecisionID)

	t.Run("refuses an archived team", func(t *testing.T) {
		old := w.team(t, "old", true)
		_, err := w.svc.UpdateTeam(ctx, ada, org, old.ID(), team.UpdateTeamProps{DisplayName: new("New")})
		require.ErrorIs(t, err, team.ErrArchived)
	})

	t.Run("refuses a missing team", func(t *testing.T) {
		_, err := w.svc.UpdateTeam(ctx, ada, org, "tem_01JZ0000000000000000000099", team.UpdateTeamProps{DisplayName: new("New")})
		require.ErrorIs(t, err, team.ErrNotFound)
	})
}

func TestArchiveAndRestoreTeam(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tm := w.team(t, "platform", false, lead(alice))

	for range 2 {
		got, err := w.svc.ArchiveTeam(ctx, ada, org, tm.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
	}
	assert.Equal(t, []string{team.EventTeamArchived}, w.auditTypes(), "archiving twice records once")
	assert.Len(t, w.teams[tm.ID()].Members, 1, "archiving keeps the members")

	w.reset()
	for range 2 {
		got, err := w.svc.RestoreTeam(ctx, ada, org, tm.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
	}
	assert.Equal(t, []string{team.EventTeamRestored}, w.auditTypes(), "restoring twice records once")
	assert.Equal(t, []string{"team.v1.team.restored"}, w.eventTypes())

	t.Run("restore refuses a slug taken meanwhile", func(t *testing.T) {
		_, err := w.svc.ArchiveTeam(ctx, ada, org, tm.ID())
		require.NoError(t, err)
		w.team(t, "platform", false)
		before := w.state.clone()

		_, err = w.svc.RestoreTeam(ctx, ada, org, tm.ID())

		require.ErrorIs(t, err, team.ErrSlugTaken)
		assert.Equal(t, before, w.state)
	})
}

func TestSetTeamMember(t *testing.T) {
	ctx := context.Background()

	t.Run("adds a user, then changes their role", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false)

		got, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleMember)
		require.NoError(t, err)
		m, ok := got.Member(bob)
		require.True(t, ok)
		assert.Equal(t, team.RoleMember, m.Role)

		_, err = w.svc.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleLead)
		require.NoError(t, err)
		assert.Equal(t, team.RoleLead, w.teams[tm.ID()].Members[0].Role)

		assert.Equal(t, []string{"team.v1.team.member_added", "team.v1.team.member_role_changed"}, w.eventTypes())
		assert.Equal(t, []string{team.EventTeamMemberSet, team.EventTeamMemberSet}, w.auditTypes())
		assert.Empty(t, w.audit[0].Before.(team.TeamSnapshot).Members)
		assert.Equal(t, "dec-"+team.ActionTeamSetMember, w.audit[0].DecisionID)
		assert.Equal(t, tm.ID(), w.audit[0].ResourceID)
	})

	t.Run("adds an agent", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false)

		got, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), releaseBot, team.RoleLead)

		require.NoError(t, err)
		m, ok := got.Member(releaseBot)
		require.True(t, ok)
		assert.Equal(t, team.MemberAgent, m.PrincipalKind, "the member's kind comes from IAM")
		assert.Equal(t, []string{releaseBot}, w.groups[tm.ID()].members)
	})

	t.Run("setting the current role records nothing", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false, lead(alice))

		got, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), alice, team.RoleLead)

		require.NoError(t, err)
		assert.Len(t, got.Members(), 1)
		assert.Empty(t, w.audit)
		assert.Empty(t, w.events)
	})

	for name, tc := range map[string]struct {
		principalID string
		archived    bool
		role        team.Role
		want        error
	}{
		"a principal not in the organization": {stranger, false, team.RoleMember, team.ErrInvalid},
		"a suspended user":                    {suspended, false, team.RoleMember, team.ErrInvalid},
		"a suspended agent":                   {pausedBot, false, team.RoleMember, team.ErrInvalid},
		"a service account":                   {ci, false, team.RoleMember, team.ErrInvalid},
		"an archived team":                    {bob, true, team.RoleMember, team.ErrArchived},
		"an archived team, same role":         {alice, true, team.RoleLead, team.ErrArchived},
		"an unknown role":                     {bob, false, "owner", team.ErrInvalid},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			w := newWorld(t)
			tm := w.team(t, "platform", tc.archived, lead(alice))
			before := w.state.clone()

			_, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), tc.principalID, tc.role)

			require.ErrorIs(t, err, tc.want)
			assert.Equal(t, before, w.state)
		})
	}

	t.Run("refuses a missing team", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.svc.SetTeamMember(ctx, ada, org, "tem_01JZ0000000000000000000099", bob, team.RoleMember)
		require.ErrorIs(t, err, team.ErrNotFound)
	})
}

func TestRemoveTeamMember(t *testing.T) {
	ctx := context.Background()

	t.Run("removes a member, even one no longer active", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false, lead(alice), team.NewTeamMemberProps{PrincipalID: suspended, PrincipalKind: team.MemberUser, Role: team.RoleMember})

		got, err := w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), suspended)

		require.NoError(t, err)
		assert.Len(t, got.Members(), 1)
		assert.Len(t, w.teams[tm.ID()].Members, 1)
		assert.Equal(t, []string{"team.v1.team.member_removed"}, w.eventTypes())
		assert.Equal(t, []string{team.EventTeamMemberRemoved}, w.auditTypes())
	})

	t.Run("refuses someone who is not a member", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false, lead(alice))
		_, err := w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), bob)
		require.ErrorIs(t, err, team.ErrNotFound)
		assert.Empty(t, w.audit)
	})

	t.Run("refuses an archived team", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", true, lead(alice))
		_, err := w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), alice)
		require.ErrorIs(t, err, team.ErrArchived)
		assert.Len(t, w.teams[tm.ID()].Members, 1)
	})
}

func TestReads(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	tm := w.team(t, "platform", false, lead(alice), team.NewTeamMemberProps{PrincipalID: bob, PrincipalKind: team.MemberUser, Role: team.RoleMember})
	w.team(t, "old", true)

	got, err := w.svc.GetTeamBySlug(ctx, ada, org, "platform")
	require.NoError(t, err)
	assert.Equal(t, tm.ID(), got.ID())

	members, err := w.svc.ListTeamMembers(ctx, ada, org, tm.ID())
	require.NoError(t, err)
	assert.Equal(t, tm.Members(), members)

	page, err := w.svc.ListTeams(ctx, ada, team.ListTeamsQuery{OrganizationID: org})
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	page, err = w.svc.ListTeams(ctx, ada, team.ListTeamsQuery{OrganizationID: org, IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)

	_, err = w.svc.GetTeam(ctx, ada, "org_other", tm.ID())
	assert.ErrorIs(t, err, team.ErrNotFound, "another Organization sees nothing")
}

// owner reports how much each Team owns in a made-up capability.
type owner map[string]int

func (o owner) ActiveOwned(_ context.Context, _, teamID string) ([]team.Owned, error) {
	return []team.Owned{{Capability: "delivery", Kind: "Application", Count: o[teamID]}}, nil
}

func TestArchiveTeamThatOwnsWork(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	s := w.store
	busy := w.team(t, "platform", false, lead(alice))
	idle := w.team(t, "tooling", false)
	svc, err := NewTeamService(append(s.options(w.authz), WithOwners(owner{busy.ID(): 2}))...)
	require.NoError(t, err)
	before := w.state.clone()

	_, err = svc.ArchiveTeam(ctx, ada, org, busy.ID())

	require.ErrorIs(t, err, team.ErrOwnsWork)
	assert.Contains(t, err.Error(), "2 active Application (delivery)")
	assert.Equal(t, before, w.state, "nothing is archived")

	got, err := svc.ArchiveTeam(ctx, ada, org, idle.ID())
	require.NoError(t, err)
	assert.True(t, got.Archived(), "a Team that owns nothing is archived")
}

func TestCreateAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("creates the agent in IAM as a member of the team", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false, lead(alice))

		got, err := w.svc.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: tm.ID(), DisplayName: "Release Agent"})
		require.NoError(t, err)

		a := got.Agent
		assert.Equal(t, "release-agent", a.Slug(), "slug derived from the display name")
		assert.Equal(t, iam.PrincipalAgent, a.Kind())
		assert.True(t, a.Active())
		assert.Equal(t, a.Snapshot(), w.agents[a.ID()], "stored through IAM")
		m, ok := got.Team.Member(a.ID())
		require.True(t, ok)
		assert.Equal(t, team.MemberAgent, m.PrincipalKind)
		assert.Equal(t, team.RoleMember, m.Role, "member by default")
		assert.Equal(t, got.Team.Snapshot(), w.teams[tm.ID()])
		assert.Equal(t, teamGroup{name: "team:platform", members: sorted(alice, a.ID())}, w.groups[tm.ID()], "the team's group gains the agent")

		assert.Equal(t, []string{"iam.v1.agent.created", "team.v1.team.member_added"}, w.eventTypes())
		assert.Equal(t, iam.AggregateAgent, w.events[0].AggregateType, "the agent's events are IAM's")
		assert.Equal(t, a.ID(), w.events[0].AggregateID)
		assert.Equal(t, team.AggregateTeam, w.events[1].AggregateType)

		assert.Equal(t, []string{team.EventTeamAgentCreated, team.EventTeamMemberSet}, w.auditTypes())
		assert.Equal(t, a.ID(), w.audit[0].ResourceID)
		assert.Equal(t, a.Snapshot(), w.audit[0].After)
		assert.Equal(t, tm.ID(), w.audit[1].ResourceID)
		assert.Equal(t, tm.Snapshot(), w.audit[1].Before)
		assert.Equal(t, got.Team.Snapshot(), w.audit[1].After)
		for _, e := range w.audit {
			assert.Equal(t, "dec-"+team.ActionTeamCreateAgent, e.DecisionID)
			assert.Equal(t, ada, e.Actor)
		}

		require.Len(t, w.authz.asked, 1)
		req := w.authz.asked[0]
		assert.Equal(t, team.PermissionAgentManage, req.Permission)
		assert.Equal(t, org, req.ResourceID, "authorized on the Organization")
	})

	t.Run("with a slug and a role", func(t *testing.T) {
		w := newWorld(t)
		tm := w.team(t, "platform", false)

		got, err := w.svc.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: tm.ID(), Slug: "rel", DisplayName: "Release", Role: team.RoleLead})

		require.NoError(t, err)
		assert.Equal(t, "rel", got.Agent.Slug())
		m, _ := got.Team.Member(got.Agent.ID())
		assert.Equal(t, team.RoleLead, m.Role)
	})

	for name, tc := range map[string]struct {
		archived bool
		teamID   string
		in       team.CreateAgentInput
		groupErr error
		want     error
	}{
		"an archived team":           {archived: true, want: team.ErrArchived},
		"a missing team":             {teamID: "tem_01JZ0000000000000000000099", want: team.ErrNotFound},
		"a slug another agent holds": {in: team.CreateAgentInput{Slug: "taken"}, want: team.ErrSlugTaken},
		"a bad slug":                 {in: team.CreateAgentInput{Slug: "Not A Slug"}, want: team.ErrInvalid},
		"no display name":            {in: team.CreateAgentInput{DisplayName: " "}, want: team.ErrInvalid},
		"an unknown role":            {in: team.CreateAgentInput{Role: "owner"}, want: team.ErrInvalid},
		"a failing group sync":       {groupErr: errors.New("connection reset")},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			w := newWorld(t)
			other := w.team(t, "other", false)
			_, err := w.svc.CreateAgent(ctx, ada, team.CreateAgentInput{OrganizationID: org, TeamID: other.ID(), Slug: "taken", DisplayName: "Taken"})
			require.NoError(t, err)
			tm := w.team(t, "platform", tc.archived, lead(alice))
			before := w.state.clone()
			w.groupErr = tc.groupErr

			in := tc.in
			in.OrganizationID, in.TeamID = org, tm.ID()
			if tc.teamID != "" {
				in.TeamID = tc.teamID
			}
			if in.DisplayName == "" {
				in.DisplayName = "Release agent"
			}
			_, err = w.svc.CreateAgent(ctx, ada, in)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.Error(t, err)
			}
			assert.Equal(t, before, w.state, "no agent without its team")
		})
	}
}

func sorted(ids ...string) []string {
	out := append([]string(nil), ids...)
	slices.Sort(out)
	return out
}

// Each Team's IAM group follows its slug and members, and loses its
// members while the Team is archived, so its grants stop applying.
func TestTeamGroupFollowsTheTeam(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	tm, err := w.svc.CreateTeam(ctx, ada, team.CreateTeamInput{OrganizationID: org, DisplayName: "Platform"})
	require.NoError(t, err)
	assert.Equal(t, teamGroup{name: "team:platform"}, w.groups[tm.ID()], "created with the team")

	_, err = w.svc.SetTeamMember(ctx, ada, org, tm.ID(), alice, team.RoleLead)
	require.NoError(t, err)
	_, err = w.svc.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleMember)
	require.NoError(t, err)
	assert.Equal(t, sorted(alice, bob), w.groups[tm.ID()].members)

	_, err = w.svc.UpdateTeam(ctx, ada, org, tm.ID(), team.UpdateTeamProps{Slug: new("core")})
	require.NoError(t, err)
	assert.Equal(t, "team:core", w.groups[tm.ID()].name, "renamed with the slug")

	_, err = w.svc.RemoveTeamMember(ctx, ada, org, tm.ID(), bob)
	require.NoError(t, err)
	assert.Equal(t, []string{alice}, w.groups[tm.ID()].members)

	_, err = w.svc.ArchiveTeam(ctx, ada, org, tm.ID())
	require.NoError(t, err)
	assert.Equal(t, teamGroup{name: "team:" + tm.ID()}, w.groups[tm.ID()], "an archived team's group has no members, and frees the slug's name")

	_, err = w.svc.RestoreTeam(ctx, ada, org, tm.ID())
	require.NoError(t, err)
	assert.Equal(t, teamGroup{name: "team:core", members: []string{alice}}, w.groups[tm.ID()], "restored with its members")

	t.Run("a failing sync rolls the change back", func(t *testing.T) {
		before := w.state.clone()
		w.groupErr = errors.New("connection reset")
		t.Cleanup(func() { w.groupErr = nil })

		_, err := w.svc.SetTeamMember(ctx, ada, org, tm.ID(), bob, team.RoleMember)

		require.Error(t, err)
		assert.Equal(t, before, w.state)
	})
}

// Every Agent stays in at least one active Team.
func TestAgentKeepsATeam(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	first := w.team(t, "platform", false, lead(alice), agentMember(releaseBot))
	second := w.team(t, "releases", false, lead(alice))

	t.Run("removing an agent from its only team is refused", func(t *testing.T) {
		before := w.state.clone()
		_, err := w.svc.RemoveTeamMember(ctx, ada, org, first.ID(), releaseBot)
		require.ErrorIs(t, err, team.ErrLastTeam)
		assert.Equal(t, before, w.state)
	})

	t.Run("archiving an agent's only team is refused", func(t *testing.T) {
		before := w.state.clone()
		_, err := w.svc.ArchiveTeam(ctx, ada, org, first.ID())
		require.ErrorIs(t, err, team.ErrLastTeam)
		assert.Contains(t, err.Error(), releaseBot)
		assert.Equal(t, before, w.state)
	})

	t.Run("an archived second team does not count", func(t *testing.T) {
		old := w.team(t, "old", true, agentMember(releaseBot))
		_, err := w.svc.RemoveTeamMember(ctx, ada, org, first.ID(), releaseBot)
		require.ErrorIs(t, err, team.ErrLastTeam)
		delete(w.teams, old.ID())
	})

	t.Run("once the agent is in another team, it can leave", func(t *testing.T) {
		_, err := w.svc.SetTeamMember(ctx, ada, org, second.ID(), releaseBot, team.RoleMember)
		require.NoError(t, err)

		_, err = w.svc.ArchiveTeam(ctx, ada, org, first.ID())
		require.NoError(t, err, "the agent keeps the second team")

		_, err = w.svc.RemoveTeamMember(ctx, ada, org, second.ID(), releaseBot)
		require.ErrorIs(t, err, team.ErrLastTeam, "the archived team no longer counts")
	})

	t.Run("users may leave their last team", func(t *testing.T) {
		_, err := w.svc.RemoveTeamMember(ctx, ada, org, second.ID(), alice)
		require.NoError(t, err)
	})
}

package team

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

var (
	t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t0.Add(2 * time.Hour)
)

const (
	org   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	alice = "prn_01ARZ3NDEKTSV4RRFFQ69G5FA1"
	bob   = "prn_01ARZ3NDEKTSV4RRFFQ69G5FA2"
	carol = "prn_01ARZ3NDEKTSV4RRFFQ69G5FA3"
)

func teamProps(members ...NewTeamMemberProps) NewTeamProps {
	return NewTeamProps{
		OrganizationID: org,
		Slug:           "platform",
		DisplayName:    "Platform",
		Description:    new("Runs the platform"),
		Members:        members,
	}
}

func newTestTeam(t *testing.T) *Team {
	t.Helper()
	tm, err := NewTeam(teamProps(), nil)
	require.NoError(t, err)
	return tm
}

func TestNewTeam(t *testing.T) {
	clock.Fix(t, t0)

	tm, err := NewTeam(teamProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("tem", tm.ID()), "a new team gets a tem_ ID: %q", tm.ID())
	assert.Equal(t, org, tm.OrganizationID())
	assert.Equal(t, "platform", tm.Slug())
	assert.Equal(t, "Platform", tm.DisplayName())
	assert.Equal(t, new("Runs the platform"), tm.Description())
	assert.Empty(t, tm.Members())
	assert.Equal(t, t0, tm.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, t0, tm.UpdatedAt())
	assert.Nil(t, tm.ArchivedAt())
	assert.False(t, tm.Archived())
}

func TestNewTeamDerivesSlug(t *testing.T) {
	props := teamProps()
	props.Slug = ""
	props.DisplayName = "Release Engineering (EU)"

	tm, err := NewTeam(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "release-engineering-eu", tm.Slug())
}

func TestNewTeamWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("tem")
	props := teamProps(
		NewTeamMemberProps{PrincipalID: bob, Role: RoleMember, CreatedAt: &t0, UpdatedAt: &t1},
		NewTeamMemberProps{PrincipalID: alice, Role: RoleLead, CreatedAt: &t0, UpdatedAt: &t0},
	)
	props.CreatedAt, props.UpdatedAt, props.ArchivedAt = &t0, &t1, &t1

	tm, err := NewTeam(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, tm.ID())
	assert.Equal(t, t0, tm.CreatedAt())
	assert.Equal(t, t1, tm.UpdatedAt())
	assert.Equal(t, &t1, tm.ArchivedAt())
	assert.True(t, tm.Archived())
	assert.Equal(t, []TeamMemberSnapshot{
		{PrincipalID: alice, Role: RoleLead, CreatedAt: t0, UpdatedAt: t0},
		{PrincipalID: bob, Role: RoleMember, CreatedAt: t0, UpdatedAt: t1},
	}, tm.Members(), "members are sorted by principal ID")
	assert.Empty(t, tm.PullEvents(), "a stored team records no event")
}

func TestNewTeamRules(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(p *NewTeamProps, teamID **string)
	}{
		"no organization":       {func(p *NewTeamProps, _ **string) { p.OrganizationID = "" }},
		"no display name":       {func(p *NewTeamProps, _ **string) { p.DisplayName = "" }},
		"a long display name":   {func(p *NewTeamProps, _ **string) { p.DisplayName = strings.Repeat("x", 256) }},
		"a bad slug":            {func(p *NewTeamProps, _ **string) { p.Slug = "Not A Slug" }},
		"an ID of another type": {func(_ *NewTeamProps, teamID **string) { *teamID = new(id.New("prn")) }},
		"a member that is not a principal": {func(p *NewTeamProps, _ **string) {
			p.Members = []NewTeamMemberProps{{PrincipalID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", Role: RoleMember}}
		}},
		"an unknown role": {func(p *NewTeamProps, _ **string) {
			p.Members = []NewTeamMemberProps{{PrincipalID: alice, Role: "owner"}}
		}},
		"a member twice": {func(p *NewTeamProps, _ **string) {
			p.Members = []NewTeamMemberProps{{PrincipalID: alice, Role: RoleMember}, {PrincipalID: alice, Role: RoleLead}}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			props := teamProps()
			var teamID *string
			tc.change(&props, &teamID)
			_, err := NewTeam(props, teamID)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestUpdateTeam(t *testing.T) {
	clock.Fix(t, t0)
	tm := newTestTeam(t)
	clock.Fix(t, t1)

	require.NoError(t, tm.Update(UpdateTeamProps{Slug: new("core"), DisplayName: new("Core"), Description: new("")}))

	assert.Equal(t, "core", tm.Slug())
	assert.Equal(t, "Core", tm.DisplayName())
	assert.Nil(t, tm.Description(), "an empty description removes it")
	assert.Equal(t, t1, tm.UpdatedAt())

	assert.ErrorIs(t, tm.Update(UpdateTeamProps{Slug: new("Bad Slug")}), ErrInvalid)
	assert.ErrorIs(t, tm.Update(UpdateTeamProps{DisplayName: new("")}), ErrInvalid)
	assert.Equal(t, "core", tm.Slug(), "nothing changes when a field is invalid")

	tm.Archive()
	assert.ErrorIs(t, tm.Update(UpdateTeamProps{DisplayName: new("Again")}), ErrArchived)
}

func TestTeamMembers(t *testing.T) {
	clock.Fix(t, t0)
	tm := newTestTeam(t)

	require.NoError(t, tm.SetMember(carol, RoleMember))
	require.NoError(t, tm.SetMember(alice, RoleLead))
	clock.Fix(t, t1)
	require.NoError(t, tm.SetMember(carol, RoleLead))

	assert.Equal(t, []TeamMemberSnapshot{
		{PrincipalID: alice, Role: RoleLead, CreatedAt: t0, UpdatedAt: t0},
		{PrincipalID: carol, Role: RoleLead, CreatedAt: t0, UpdatedAt: t1},
	}, tm.Members(), "a role change keeps when the member joined")
	m, ok := tm.Member(carol)
	assert.True(t, ok)
	assert.Equal(t, RoleLead, m.Role)
	_, ok = tm.Member(bob)
	assert.False(t, ok)
	assert.Equal(t, t1, tm.UpdatedAt())

	assert.ErrorIs(t, tm.SetMember("not-a-principal", RoleMember), ErrInvalid)
	assert.ErrorIs(t, tm.SetMember(bob, "owner"), ErrInvalid)

	require.NoError(t, tm.RemoveMember(alice))
	assert.Len(t, tm.Members(), 1)
	assert.ErrorIs(t, tm.RemoveMember(alice), ErrNotFound)

	tm.Archive()
	assert.ErrorIs(t, tm.SetMember(bob, RoleMember), ErrArchived, "an archived team cannot change members")
	assert.ErrorIs(t, tm.RemoveMember(carol), ErrArchived)
	tm.Restore()
	require.NoError(t, tm.RemoveMember(carol))
}

func TestSnapshotsDoNotShareState(t *testing.T) {
	tm := newTestTeam(t)
	require.NoError(t, tm.SetMember(alice, RoleLead))

	s := tm.Snapshot()
	*s.Description = "changed"
	s.Members[0].Role = RoleMember

	assert.Equal(t, new("Runs the platform"), tm.Description())
	m, _ := tm.Member(alice)
	assert.Equal(t, RoleLead, m.Role)
}

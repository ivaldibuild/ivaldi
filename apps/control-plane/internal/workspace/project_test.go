package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func validProjectProps() NewProjectProps {
	return NewProjectProps{OrganizationID: testOrg, WorkspaceID: "wsp_1", OwnerTeamID: testTeam, Slug: "checkout", DisplayName: "Checkout", CreatedByPrincipalID: "prn_alice"}
}

func newTestProject(t *testing.T) *Project {
	t.Helper()
	p, err := NewProject(validProjectProps(), nil)
	require.NoError(t, err)
	return p
}

func TestNewProject(t *testing.T) {
	fixClock(t, t0)

	p, err := NewProject(validProjectProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid(Project{}.Prefix(), p.ID()), "a new project gets a prefixed ID: %q", p.ID())
	assert.Equal(t, testOrg, p.OrganizationID())
	assert.Equal(t, "wsp_1", p.WorkspaceID())
	assert.Equal(t, testTeam, p.OwnerTeamID())
	assert.Equal(t, "checkout", p.Slug())
	assert.Equal(t, "Checkout", p.DisplayName())
	assert.Equal(t, "prn_alice", p.CreatedByPrincipalID())
	assert.Equal(t, t0, p.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, t0, p.UpdatedAt())
	assert.False(t, p.Archived())
}

func TestNewProjectIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		change func(*NewProjectProps)
	}{
		{"no organization", func(p *NewProjectProps) { p.OrganizationID = "" }},
		{"no workspace", func(p *NewProjectProps) { p.WorkspaceID = "" }},
		{"no owning team", func(p *NewProjectProps) { p.OwnerTeamID = "" }},
		{"invalid slug", func(p *NewProjectProps) { p.Slug = "Check Out" }},
		{"no display name", func(p *NewProjectProps) { p.DisplayName = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := validProjectProps()
			tt.change(&props)
			_, err := NewProject(props, nil)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestProjectUpdate(t *testing.T) {
	fixClock(t, t0)
	p := newTestProject(t)
	fixClock(t, t1)

	require.NoError(t, p.Update(UpdateProjectProps{Slug: new("payments"), Description: new("Takes money")}))

	assert.Equal(t, "payments", p.Slug())
	assert.Equal(t, "Takes money", p.Description())
	assert.Equal(t, "Checkout", p.DisplayName(), "unset fields are left alone")
	assert.Equal(t, "wsp_1", p.WorkspaceID(), "a project's workspace never changes")
	assert.Equal(t, t1, p.UpdatedAt())

	before := p.Snapshot()
	require.ErrorIs(t, p.Update(UpdateProjectProps{DisplayName: new("")}), ErrInvalid)
	assert.Equal(t, before, p.Snapshot(), "an invalid update changes nothing")
}

func TestProjectTransferTo(t *testing.T) {
	p := newTestProject(t)
	fixClock(t, t1)

	require.NoError(t, p.TransferTo("tem_2"))
	assert.Equal(t, "tem_2", p.OwnerTeamID())
	assert.Equal(t, t1, p.UpdatedAt())

	require.ErrorIs(t, p.TransferTo(""), ErrInvalid)
	assert.Equal(t, "tem_2", p.OwnerTeamID())
}

func TestArchivedProjectCannotChange(t *testing.T) {
	p := newTestProject(t)
	p.Archive()

	require.ErrorIs(t, p.Update(UpdateProjectProps{DisplayName: new("X")}), ErrArchived)
	require.ErrorIs(t, p.TransferTo("tem_2"), ErrArchived)
	assert.Equal(t, testTeam, p.OwnerTeamID())
}

func TestProjectArchiveAndRestore(t *testing.T) {
	p := newTestProject(t)
	fixClock(t, t1)

	p.Archive()
	require.True(t, p.Archived())
	assert.Equal(t, t1, *p.ArchivedAt())

	fixClock(t, t1.Add(time.Hour))
	p.Archive()
	assert.Equal(t, t1, *p.ArchivedAt(), "archiving again does nothing")

	p.Restore()
	assert.False(t, p.Archived())
	assert.Equal(t, t1.Add(time.Hour), p.UpdatedAt())
}

func TestNewProjectRebuildsAStoredProject(t *testing.T) {
	archived := t1
	d := ProjectSnapshot{
		ID: "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV", OrganizationID: testOrg, WorkspaceID: "wsp_1", OwnerTeamID: testTeam,
		Slug: "checkout", DisplayName: "Checkout", Description: "Stored", CreatedByPrincipalID: "prn_alice",
		CreatedAt: t0, UpdatedAt: t1, ArchivedAt: &archived,
	}

	p, err := NewProject(NewProjectProps{
		OrganizationID: d.OrganizationID, WorkspaceID: d.WorkspaceID, OwnerTeamID: d.OwnerTeamID,
		Slug: d.Slug, DisplayName: d.DisplayName, Description: d.Description, CreatedByPrincipalID: d.CreatedByPrincipalID,
		CreatedAt: &d.CreatedAt, UpdatedAt: &d.UpdatedAt, ArchivedAt: d.ArchivedAt,
	}, &d.ID)

	require.NoError(t, err)
	assert.Equal(t, d, p.Snapshot())

	bad := "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err = NewProject(validProjectProps(), &bad)
	assert.ErrorIs(t, err, ErrInvalid, "a stored ID must be a project's")
	*p.ArchivedAt() = t0
	assert.Equal(t, t1, *p.ArchivedAt(), "getters return copies")
}

package workspace

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
	t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

func newTestWorkspace(t *testing.T, slug string) *Workspace {
	t.Helper()
	w, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: slug, DisplayName: slug}, nil)
	require.NoError(t, err)
	return w
}

func TestNewWorkspace(t *testing.T) {
	clock.Fix(t, t0)

	w, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform", Description: "Core", CreatedByPrincipalID: "prn_alice"}, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid(Workspace{}.Prefix(), w.ID()), "a new workspace gets a prefixed ID: %q", w.ID())
	assert.Equal(t, testOrg, w.OrganizationID())
	assert.Equal(t, "platform", w.Slug())
	assert.Equal(t, "Platform", w.DisplayName())
	assert.Equal(t, "Core", w.Description())
	assert.Equal(t, "prn_alice", w.CreatedByPrincipalID())
	assert.Equal(t, t0, w.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, t0, w.UpdatedAt())
	assert.False(t, w.IsDefault())
	assert.False(t, w.Archived())
	assert.Nil(t, w.ArchivedAt())
}

func TestNewWorkspaceWithID(t *testing.T) {
	given := "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"

	w, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform"}, &given)

	require.NoError(t, err)
	assert.Equal(t, given, w.ID(), "a supplied ID is kept")
}

func TestNewWorkspaceIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		props NewWorkspaceProps
	}{
		{"no organization", NewWorkspaceProps{Slug: "platform", DisplayName: "Platform"}},
		{"uppercase slug", NewWorkspaceProps{OrganizationID: testOrg, Slug: "Platform", DisplayName: "Platform"}},
		{"double hyphen", NewWorkspaceProps{OrganizationID: testOrg, Slug: "a--b", DisplayName: "Platform"}},
		{"slug too long", NewWorkspaceProps{OrganizationID: testOrg, Slug: strings.Repeat("a", maxSlugLength+1), DisplayName: "Platform"}},
		{"display name too long", NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform", DisplayName: strings.Repeat("é", maxDisplayNameLength+1)}},
		{"no display name", NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWorkspace(tt.props, nil)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("limits are inclusive", func(t *testing.T) {
		_, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: strings.Repeat("a", maxSlugLength), DisplayName: strings.Repeat("é", maxDisplayNameLength)}, nil)
		require.NoError(t, err, "a %d-character slug and a %d-character display name are allowed; names count characters, not bytes", maxSlugLength, maxDisplayNameLength)
	})
}

func TestNewDefaultWorkspace(t *testing.T) {
	w, err := NewDefaultWorkspace(testOrg)

	require.NoError(t, err)
	assert.True(t, w.IsDefault())
	assert.Equal(t, DefaultWorkspaceSlug, w.Slug())
	assert.Equal(t, DefaultWorkspaceName, w.DisplayName())
	assert.Empty(t, w.CreatedByPrincipalID(), "the system creates the default workspace")

	_, err = NewDefaultWorkspace("")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestWorkspaceUpdate(t *testing.T) {
	clock.Fix(t, t0)
	w := newTestWorkspace(t, "platform")
	clock.Fix(t, t1)

	require.NoError(t, w.Update(UpdateWorkspaceProps{DisplayName: new("Core"), Description: new("Shared services")}))

	assert.Equal(t, "Core", w.DisplayName())
	assert.Equal(t, "Shared services", w.Description())
	assert.Equal(t, "platform", w.Slug(), "unset fields are left alone")
	assert.Equal(t, t0, w.CreatedAt())
	assert.Equal(t, t1, w.UpdatedAt())

	t.Run("an invalid update changes nothing", func(t *testing.T) {
		before := w.Snapshot()
		require.ErrorIs(t, w.Update(UpdateWorkspaceProps{Slug: new("Not A Slug"), DisplayName: new("Changed")}), ErrInvalid)
		assert.Equal(t, before, w.Snapshot())
	})

	t.Run("an archived workspace cannot be updated", func(t *testing.T) {
		require.NoError(t, w.Archive())
		require.ErrorIs(t, w.Update(UpdateWorkspaceProps{DisplayName: new("X")}), ErrArchived)
	})
}

func TestWorkspaceDefault(t *testing.T) {
	def, err := NewDefaultWorkspace(testOrg)
	require.NoError(t, err)

	require.ErrorIs(t, def.Archive(), ErrDefaultWorkspace, "the default workspace cannot be archived")
	assert.False(t, def.Archived())

	def.ClearDefault()
	assert.False(t, def.IsDefault())
	require.NoError(t, def.Archive(), "once it is no longer the default it can be archived")

	require.ErrorIs(t, def.MakeDefault(), ErrArchived, "an archived workspace cannot become the default")
	assert.False(t, def.IsDefault())

	def.Restore()
	require.NoError(t, def.MakeDefault())
	assert.True(t, def.IsDefault())
}

func TestWorkspaceArchiveAndRestore(t *testing.T) {
	w := newTestWorkspace(t, "platform")
	clock.Fix(t, t1)

	require.NoError(t, w.Archive())
	require.True(t, w.Archived())
	assert.Equal(t, t1, *w.ArchivedAt())
	assert.Equal(t, t1, w.UpdatedAt())

	clock.Fix(t, t1.Add(time.Hour))
	require.NoError(t, w.Archive(), "archiving again does nothing")
	assert.Equal(t, t1, *w.ArchivedAt(), "the first archive time is kept")
	assert.Equal(t, t1, w.UpdatedAt())

	w.Restore()
	assert.False(t, w.Archived())
	assert.Nil(t, w.ArchivedAt())
	assert.Equal(t, t1.Add(time.Hour), w.UpdatedAt())

	before := w.UpdatedAt()
	w.Restore()
	assert.Equal(t, before, w.UpdatedAt(), "restoring an active workspace does nothing")
}

func TestWorkspaceStateCannotBeChangedFromOutside(t *testing.T) {
	w := newTestWorkspace(t, "platform")
	require.NoError(t, w.Archive())
	archivedAt := *w.ArchivedAt()

	*w.ArchivedAt() = t0
	d := w.Snapshot()
	d.Slug = "changed"
	*d.ArchivedAt = t0

	assert.Equal(t, archivedAt, *w.ArchivedAt(), "getters and Snapshot return copies")
	assert.Equal(t, "platform", w.Slug())
}

func TestNewWorkspaceRebuildsAStoredWorkspace(t *testing.T) {
	archived := t1
	d := WorkspaceSnapshot{
		ID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", OrganizationID: testOrg, Slug: "default", DisplayName: "Default",
		Description: "Stored", IsDefault: true, CreatedByPrincipalID: "prn_alice", CreatedAt: t0, UpdatedAt: t1, ArchivedAt: &archived,
	}

	w, err := NewWorkspace(NewWorkspaceProps{
		OrganizationID: d.OrganizationID, Slug: d.Slug, DisplayName: d.DisplayName, Description: d.Description,
		IsDefault: d.IsDefault, CreatedByPrincipalID: d.CreatedByPrincipalID,
		CreatedAt: &d.CreatedAt, UpdatedAt: &d.UpdatedAt, ArchivedAt: d.ArchivedAt,
	}, &d.ID)

	require.NoError(t, err)
	assert.Equal(t, d, w.Snapshot(), "stored state comes back as it was, even an archived default the methods would refuse")
	archived = t0
	assert.Equal(t, t1, *w.ArchivedAt(), "changing the props afterwards does not change the workspace")
}

func TestNewWorkspaceRefusesAMalformedStoredID(t *testing.T) {
	for _, given := range []string{"wsp_1", "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV", ""} {
		_, err := NewWorkspace(NewWorkspaceProps{OrganizationID: testOrg, Slug: "platform", DisplayName: "Platform"}, &given)

		assert.ErrorIs(t, err, ErrInvalid, "stored ID %q", given)
	}
}

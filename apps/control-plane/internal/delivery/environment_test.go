package delivery

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Shared fixtures for the domain tests. They carry the dom prefix so they do
// not collide with the service tests' helpers in the same package.
var (
	domT0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	domT1 = domT0.Add(time.Hour)
	domT2 = domT0.Add(2 * time.Hour)
)

const (
	domOrg       = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	domWorkspace = "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

func domEnvironmentProps() NewEnvironmentProps {
	return NewEnvironmentProps{
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "staging",
		DisplayName:    "Staging",
		Tier:           TierStaging,
	}
}

func domNewEnvironment(t *testing.T) *Environment {
	t.Helper()
	e, err := NewEnvironment(domEnvironmentProps(), nil)
	require.NoError(t, err)
	return e
}

func TestNewEnvironment(t *testing.T) {
	clock.Fix(t, domT0)

	props := domEnvironmentProps()
	props.ExecutionEnabled = true
	e, err := NewEnvironment(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("env", e.ID()), "a new environment gets an env_ ID: %q", e.ID())
	assert.Equal(t, domOrg, e.OrganizationID())
	assert.Equal(t, domWorkspace, e.WorkspaceID())
	assert.Equal(t, "staging", e.Slug())
	assert.Equal(t, "Staging", e.DisplayName())
	assert.Equal(t, TierStaging, e.Tier())
	assert.True(t, e.ExecutionEnabled())
	assert.Equal(t, domT0, e.CreatedAt(), "timestamps come from the domain clock")
	assert.Equal(t, domT0, e.UpdatedAt())
	assert.Nil(t, e.DeletedAt())
	assert.False(t, e.Archived())
}

func TestNewEnvironmentDerivesSlug(t *testing.T) {
	props := domEnvironmentProps()
	props.Slug = ""
	props.DisplayName = "Production EU (west)"

	e, err := NewEnvironment(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "production-eu-west", e.Slug())
}

func TestNewEnvironmentWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("env")
	props := domEnvironmentProps()
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.DeletedAt = new(domT1)

	e, err := NewEnvironment(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, e.ID(), "a given ID is kept")
	assert.Equal(t, domT0, e.CreatedAt(), "given timestamps are kept")
	assert.Equal(t, domT1, e.UpdatedAt())
	require.NotNil(t, e.DeletedAt())
	assert.Equal(t, domT1, *e.DeletedAt())
	assert.True(t, e.Archived())

	*props.DeletedAt = domT2
	assert.Equal(t, domT1, *e.DeletedAt(), "the given time is copied")
}

func TestNewEnvironmentIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewEnvironmentProps)
		id     *string
	}{
		{"no organization", func(p *NewEnvironmentProps) { p.OrganizationID = "" }, nil},
		{"no workspace", func(p *NewEnvironmentProps) { p.WorkspaceID = "" }, nil},
		{"no display name", func(p *NewEnvironmentProps) { p.DisplayName = "" }, nil},
		{"display name too long", func(p *NewEnvironmentProps) { p.DisplayName = strings.Repeat("é", maxDisplayNameLength+1) }, nil},
		{"uppercase slug", func(p *NewEnvironmentProps) { p.Slug = "Staging" }, nil},
		{"double hyphen slug", func(p *NewEnvironmentProps) { p.Slug = "a--b" }, nil},
		{"slug too long", func(p *NewEnvironmentProps) { p.Slug = strings.Repeat("a", 64) }, nil},
		{"underivable slug", func(p *NewEnvironmentProps) { p.Slug = ""; p.DisplayName = "!!!" }, nil},
		{"empty tier", func(p *NewEnvironmentProps) { p.Tier = "" }, nil},
		{"unknown tier", func(p *NewEnvironmentProps) { p.Tier = "qa" }, nil},
		{"ID with wrong prefix", func(*NewEnvironmentProps) {}, new(id.New("lfc"))},
		{"malformed ID", func(*NewEnvironmentProps) {}, new("env_nope")},
		{"empty ID", func(*NewEnvironmentProps) {}, new("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domEnvironmentProps()
			tt.mutate(&props)
			_, err := NewEnvironment(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestNewEnvironmentAcceptsEveryTier(t *testing.T) {
	for _, tier := range []Tier{TierDevelopment, TierStaging, TierProduction, TierOther} {
		t.Run(string(tier), func(t *testing.T) {
			props := domEnvironmentProps()
			props.Tier = tier
			e, err := NewEnvironment(props, nil)
			require.NoError(t, err)
			assert.Equal(t, tier, e.Tier())
		})
	}

	t.Run("display name limit counts characters", func(t *testing.T) {
		props := domEnvironmentProps()
		props.DisplayName = strings.Repeat("é", maxDisplayNameLength)
		_, err := NewEnvironment(props, nil)
		require.NoError(t, err)
	})
}

func TestEnvironmentGettersReturnCopies(t *testing.T) {
	props := domEnvironmentProps()
	props.DeletedAt = new(domT0)
	e, err := NewEnvironment(props, nil)
	require.NoError(t, err)

	*e.DeletedAt() = domT2
	snap := e.Snapshot()
	*snap.DeletedAt = domT2

	assert.Equal(t, domT0, *e.DeletedAt())
}

func TestEnvironmentSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	e := domNewEnvironment(t)

	assert.Equal(t, EnvironmentSnapshot{
		ID:             e.ID(),
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "staging",
		DisplayName:    "Staging",
		Tier:           TierStaging,
		CreatedAt:      domT0,
		UpdatedAt:      domT0,
	}, e.Snapshot())
}

func TestEnvironmentUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	e := domNewEnvironment(t)
	clock.Fix(t, domT1)

	require.NoError(t, e.Update(UpdateEnvironmentProps{
		Slug:             new("prod"),
		DisplayName:      new("Prod"),
		Tier:             new(TierProduction),
		ExecutionEnabled: new(true),
	}))

	assert.Equal(t, "prod", e.Slug())
	assert.Equal(t, "Prod", e.DisplayName())
	assert.Equal(t, TierProduction, e.Tier())
	assert.True(t, e.ExecutionEnabled())
	assert.Equal(t, domT0, e.CreatedAt())
	assert.Equal(t, domT1, e.UpdatedAt())

	t.Run("unset fields are kept", func(t *testing.T) {
		require.NoError(t, e.Update(UpdateEnvironmentProps{DisplayName: new("Production")}))
		assert.Equal(t, "prod", e.Slug())
		assert.Equal(t, "Production", e.DisplayName())
		assert.Equal(t, TierProduction, e.Tier())
		assert.True(t, e.ExecutionEnabled())
	})
}

func TestEnvironmentUpdateIsAtomic(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateEnvironmentProps
	}{
		{"bad slug", UpdateEnvironmentProps{Slug: new("Bad Slug"), DisplayName: new("New"), Tier: new(TierProduction), ExecutionEnabled: new(true)}},
		{"bad display name", UpdateEnvironmentProps{Slug: new("new"), DisplayName: new(""), Tier: new(TierProduction), ExecutionEnabled: new(true)}},
		{"bad tier", UpdateEnvironmentProps{Slug: new("new"), DisplayName: new("New"), Tier: new(Tier("qa")), ExecutionEnabled: new(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			e := domNewEnvironment(t)
			before := e.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, e.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, e.Snapshot(), "a failed update changes nothing")
		})
	}
}

func TestEnvironmentUpdateRefusesArchived(t *testing.T) {
	clock.Fix(t, domT0)
	e := domNewEnvironment(t)
	e.Archive()
	before := e.Snapshot()

	err := e.Update(UpdateEnvironmentProps{DisplayName: new("New")})

	assert.ErrorIs(t, err, ErrArchived)
	assert.Equal(t, before, e.Snapshot())
}

func TestEnvironmentArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	e := domNewEnvironment(t)

	clock.Fix(t, domT1)
	e.Archive()
	assert.True(t, e.Archived())
	require.NotNil(t, e.DeletedAt())
	assert.Equal(t, domT1, *e.DeletedAt())
	assert.Equal(t, domT1, e.UpdatedAt())

	clock.Fix(t, domT2)
	e.Archive()
	assert.Equal(t, domT1, *e.DeletedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, e.UpdatedAt())

	e.Restore()
	assert.False(t, e.Archived())
	assert.Nil(t, e.DeletedAt())
	assert.Equal(t, domT2, e.UpdatedAt())

	clock.Fix(t, domT2.Add(time.Hour))
	e.Restore()
	assert.Equal(t, domT2, e.UpdatedAt(), "restoring an active environment does nothing")
}

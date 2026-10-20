package delivery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func domArtifactProps() NewArtifactProps {
	return NewArtifactProps{
		OrganizationID: domOrg,
		ApplicationID:  id.New("app"),
		Name:           "api",
		ArtifactKind:   ArtifactContainerImage,
		ArtifactName:   "ghcr.io/acme/api",
		Required:       true,
	}
}

func domNewArtifact(t *testing.T) *Artifact {
	t.Helper()
	a, err := NewArtifact(domArtifactProps(), nil)
	require.NoError(t, err)
	return a
}

func TestNewArtifact(t *testing.T) {
	clock.Fix(t, domT0)
	props := domArtifactProps()
	registryID := id.New("reg")
	props.RegistryID = &registryID

	a, err := NewArtifact(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("art", a.ID()), "a new artifact gets an art_ ID: %q", a.ID())
	assert.Equal(t, domOrg, a.OrganizationID())
	assert.Equal(t, props.ApplicationID, a.ApplicationID())
	assert.Equal(t, "api", a.Name())
	assert.Equal(t, ArtifactContainerImage, a.ArtifactKind())
	assert.Equal(t, "ghcr.io/acme/api", a.ArtifactName())
	assert.Equal(t, registryID, *a.RegistryID())
	assert.True(t, a.Required())
	assert.Equal(t, ArtifactDeclared, a.Source(), "the source defaults to declared")
	assert.Equal(t, domT0, a.CreatedAt())
	assert.Equal(t, domT0, a.UpdatedAt())
	assert.Nil(t, a.ArchivedAt())
	assert.False(t, a.Archived())
}

func TestNewArtifactOptions(t *testing.T) {
	for _, kind := range []ArtifactKind{ArtifactContainerImage, ArtifactHelmChart, ArtifactArchive} {
		t.Run(string(kind), func(t *testing.T) {
			props := domArtifactProps()
			props.ArtifactKind = kind
			a, err := NewArtifact(props, nil)
			require.NoError(t, err)
			assert.Equal(t, kind, a.ArtifactKind())
		})
	}

	for _, source := range []ArtifactSource{ArtifactDeclared, ArtifactObserved} {
		t.Run(string(source), func(t *testing.T) {
			props := domArtifactProps()
			props.Source = source
			a, err := NewArtifact(props, nil)
			require.NoError(t, err)
			assert.Equal(t, source, a.Source())
		})
	}

	t.Run("empty registry is none", func(t *testing.T) {
		props := domArtifactProps()
		props.RegistryID = new("")
		a, err := NewArtifact(props, nil)
		require.NoError(t, err)
		assert.Nil(t, a.RegistryID())
	})

	t.Run("published name at its limit", func(t *testing.T) {
		props := domArtifactProps()
		props.ArtifactName = strings.Repeat("a", maxPublishedNameLength)
		_, err := NewArtifact(props, nil)
		require.NoError(t, err)
	})

	t.Run("published name with a port and path", func(t *testing.T) {
		props := domArtifactProps()
		props.ArtifactName = "registry.internal:5000/acme/api"
		_, err := NewArtifact(props, nil)
		require.NoError(t, err)
	})
}

func TestNewArtifactWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("art")
	props := domArtifactProps()
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.ArchivedAt = new(domT1)

	a, err := NewArtifact(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, a.ID())
	assert.Equal(t, domT0, a.CreatedAt())
	assert.Equal(t, domT1, a.UpdatedAt())
	assert.Equal(t, domT1, *a.ArchivedAt())
	assert.True(t, a.Archived())
}

func TestNewArtifactIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewArtifactProps)
		id     *string
	}{
		{"no organization", func(p *NewArtifactProps) { p.OrganizationID = "" }, nil},
		{"no application", func(p *NewArtifactProps) { p.ApplicationID = "" }, nil},
		{"application ID with wrong prefix", func(p *NewArtifactProps) { p.ApplicationID = id.New("art") }, nil},
		{"ID with wrong prefix", func(*NewArtifactProps) {}, new(id.New("app"))},
		{"malformed ID", func(*NewArtifactProps) {}, new("art_x")},
		{"no name", func(p *NewArtifactProps) { p.Name = "" }, nil},
		{"uppercase name", func(p *NewArtifactProps) { p.Name = "API" }, nil},
		{"name with an underscore", func(p *NewArtifactProps) { p.Name = "api_image" }, nil},
		{"name with a slash", func(p *NewArtifactProps) { p.Name = "acme/api" }, nil},
		{"name too long", func(p *NewArtifactProps) { p.Name = strings.Repeat("a", 64) }, nil},
		{"no kind", func(p *NewArtifactProps) { p.ArtifactKind = "" }, nil},
		{"unknown kind", func(p *NewArtifactProps) { p.ArtifactKind = "jar" }, nil},
		{"no published name", func(p *NewArtifactProps) { p.ArtifactName = "" }, nil},
		{"published name with a digest", func(p *NewArtifactProps) { p.ArtifactName = "ghcr.io/acme/api@sha256:abc" }, nil},
		{"published name with a space", func(p *NewArtifactProps) { p.ArtifactName = "ghcr.io/acme/my api" }, nil},
		{"published name with a newline", func(p *NewArtifactProps) { p.ArtifactName = "ghcr.io/acme/api\n" }, nil},
		{"published name with a scheme", func(p *NewArtifactProps) { p.ArtifactName = "https://ghcr.io/acme/api" }, nil},
		{"published name too long", func(p *NewArtifactProps) { p.ArtifactName = strings.Repeat("a", maxPublishedNameLength+1) }, nil},
		{"registry ID with wrong prefix", func(p *NewArtifactProps) { p.RegistryID = new(id.New("env")) }, nil},
		{"malformed registry ID", func(p *NewArtifactProps) { p.RegistryID = new("ghcr") }, nil},
		{"unknown source", func(p *NewArtifactProps) { p.Source = "imported" }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domArtifactProps()
			tt.mutate(&props)
			_, err := NewArtifact(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestArtifactGettersReturnCopies(t *testing.T) {
	props := domArtifactProps()
	props.RegistryID = new(id.New("reg"))
	props.ArchivedAt = new(domT0)
	a, err := NewArtifact(props, nil)
	require.NoError(t, err)
	before := a.Snapshot()

	*props.RegistryID = "changed"
	*props.ArchivedAt = domT2
	*a.RegistryID() = "changed"
	*a.ArchivedAt() = domT2
	snap := a.Snapshot()
	*snap.RegistryID = "changed"
	*snap.ArchivedAt = domT2

	assert.Equal(t, before, a.Snapshot())
}

func TestArtifactSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	props := domArtifactProps()
	a, err := NewArtifact(props, nil)
	require.NoError(t, err)

	assert.Equal(t, ArtifactSnapshot{
		ID:             a.ID(),
		OrganizationID: domOrg,
		ApplicationID:  props.ApplicationID,
		Name:           "api",
		ArtifactKind:   ArtifactContainerImage,
		ArtifactName:   "ghcr.io/acme/api",
		Required:       true,
		Source:         ArtifactDeclared,
		CreatedAt:      domT0,
		UpdatedAt:      domT0,
	}, a.Snapshot())
}

func TestArtifactUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewArtifact(t)
	clock.Fix(t, domT1)

	require.NoError(t, a.Update(UpdateArtifactProps{Name: new("worker"), ArtifactName: new("ghcr.io/acme/worker"), Required: new(false)}))

	assert.Equal(t, "worker", a.Name())
	assert.Equal(t, "ghcr.io/acme/worker", a.ArtifactName())
	assert.False(t, a.Required())
	assert.Equal(t, ArtifactContainerImage, a.ArtifactKind(), "the kind never changes")
	assert.Equal(t, domT1, a.UpdatedAt())

	t.Run("unset fields are kept", func(t *testing.T) {
		require.NoError(t, a.Update(UpdateArtifactProps{Required: new(true)}))
		assert.Equal(t, "worker", a.Name())
		assert.Equal(t, "ghcr.io/acme/worker", a.ArtifactName())
		assert.True(t, a.Required())
	})
}

func TestArtifactUpdateIsAtomic(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateArtifactProps
	}{
		{"bad name", UpdateArtifactProps{Name: new("Worker"), ArtifactName: new("ghcr.io/acme/worker"), Required: new(false)}},
		{"empty name", UpdateArtifactProps{Name: new(""), ArtifactName: new("ghcr.io/acme/worker"), Required: new(false)}},
		{"published name with a digest", UpdateArtifactProps{Name: new("worker"), ArtifactName: new("ghcr.io/acme/worker@sha256:abc"), Required: new(false)}},
		{"published name with a scheme", UpdateArtifactProps{Name: new("worker"), ArtifactName: new("oci://ghcr.io/acme/worker"), Required: new(false)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			a := domNewArtifact(t)
			before := a.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, a.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, a.Snapshot())
		})
	}
}

func TestArtifactRefusesChangesWhenArchived(t *testing.T) {
	props := domArtifactProps()
	props.Source = ArtifactObserved
	a, err := NewArtifact(props, nil)
	require.NoError(t, err)
	a.Archive()
	before := a.Snapshot()

	assert.ErrorIs(t, a.Update(UpdateArtifactProps{Name: new("worker")}), ErrArchived)
	assert.ErrorIs(t, a.UseRegistry(id.New("reg")), ErrArchived)
	assert.ErrorIs(t, a.ClearRegistry(), ErrArchived)
	assert.ErrorIs(t, a.Declare(), ErrArchived)
	assert.Equal(t, before, a.Snapshot())
}

func TestArtifactRegistry(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewArtifact(t)
	registryID := id.New("reg")

	clock.Fix(t, domT1)
	require.NoError(t, a.UseRegistry(registryID))
	assert.Equal(t, registryID, *a.RegistryID())
	assert.Equal(t, domT1, a.UpdatedAt())

	for _, bad := range []string{"", "ghcr", id.New("env")} {
		t.Run("refuses "+bad, func(t *testing.T) {
			before := a.Snapshot()
			assert.ErrorIs(t, a.UseRegistry(bad), ErrInvalid)
			assert.Equal(t, before, a.Snapshot())
		})
	}

	clock.Fix(t, domT2)
	require.NoError(t, a.ClearRegistry())
	assert.Nil(t, a.RegistryID())
	assert.Equal(t, domT2, a.UpdatedAt())
}

func TestArtifactDeclare(t *testing.T) {
	clock.Fix(t, domT0)
	props := domArtifactProps()
	props.Source = ArtifactObserved
	a, err := NewArtifact(props, nil)
	require.NoError(t, err)

	clock.Fix(t, domT1)
	require.NoError(t, a.Declare())
	assert.Equal(t, ArtifactDeclared, a.Source())
	assert.Equal(t, domT1, a.UpdatedAt())

	clock.Fix(t, domT2)
	require.NoError(t, a.Declare())
	assert.Equal(t, domT1, a.UpdatedAt(), "declaring a declared artifact does nothing")
}

func TestArtifactArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewArtifact(t)

	clock.Fix(t, domT1)
	a.Archive()
	assert.True(t, a.Archived())
	assert.Equal(t, domT1, *a.ArchivedAt())
	assert.Equal(t, domT1, a.UpdatedAt())

	clock.Fix(t, domT2)
	a.Archive()
	assert.Equal(t, domT1, *a.ArchivedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, a.UpdatedAt())

	a.Restore()
	assert.False(t, a.Archived())
	assert.Nil(t, a.ArchivedAt())
	assert.Equal(t, domT2, a.UpdatedAt())

	clock.Fix(t, domT0)
	a.Restore()
	assert.Equal(t, domT2, a.UpdatedAt(), "restoring an active artifact does nothing")
}

package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

func TestRegistryRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip", func(t *testing.T) {
		reg := must(delivery.NewRegistry(delivery.NewRegistryProps{
			OrganizationID: orgA,
			WorkspaceID:    wsA,
			Slug:           "internal",
			DisplayName:    "Internal",
			Kind:           delivery.RegistryOCI,
			URL:            "https://registry.internal:5000/v2",
			CredentialRef:  new("cred_registry"),
			LookupFrom:     delivery.LookupFromDataPlane,
			LookupPool:     new("eu-pool"),
		}, nil))
		require.NoError(t, f.registries.CreateRegistry(ctx, reg))

		got, err := f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())

		bySlug, err := f.registries.GetRegistryBySlug(ctx, orgA, wsA, "internal")
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip with defaults", func(t *testing.T) {
		reg := f.createRegistry(t, orgA, wsA, "public")
		got, err := f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())
		assert.Equal(t, delivery.LookupFromControlPlane, got.LookupFrom())
		assert.Nil(t, got.CredentialRef())
		assert.Nil(t, got.LookupPool())
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		reg := f.createRegistry(t, orgA, wsA, "isolated")
		_, err := f.registries.GetRegistry(ctx, orgB, reg.ID())
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = f.registries.GetRegistryBySlug(ctx, orgB, wsA, "isolated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)
		page, err := f.registries.ListRegistries(ctx, delivery.ListRegistriesQuery{OrganizationID: orgB, WorkspaceID: wsA})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		f.createRegistry(t, orgB, wsA, "isolated")
	})

	t.Run("update writes the whole state", func(t *testing.T) {
		reg := f.createRegistry(t, orgA, wsA, "to-update")

		at(t, time.Minute)
		require.NoError(t, reg.Update(delivery.UpdateRegistryProps{Slug: new("updated"), DisplayName: new("Updated"), URL: new("http://mirror.example.com")}))
		require.NoError(t, reg.SetCredential("cred_new"))
		require.NoError(t, reg.SetLookup(delivery.LookupFromDataPlane, "pool-1"))
		require.NoError(t, f.registries.UpdateRegistry(ctx, reg))
		got, err := f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())

		at(t, 2*time.Minute)
		require.NoError(t, reg.ClearCredential())
		require.NoError(t, reg.SetLookup(delivery.LookupFromControlPlane, ""))
		require.NoError(t, f.registries.UpdateRegistry(ctx, reg))
		got, err = f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())
		assert.Nil(t, got.CredentialRef(), "clearing writes NULL")
		assert.Nil(t, got.LookupPool())

		at(t, 3*time.Minute)
		reg.Archive()
		require.NoError(t, f.registries.UpdateRegistry(ctx, reg))
		got, err = f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())
		_, err = f.registries.GetRegistryBySlug(ctx, orgA, wsA, "updated")
		assert.ErrorIs(t, err, delivery.ErrNotFound)

		at(t, 4*time.Minute)
		reg.Restore()
		require.NoError(t, f.registries.UpdateRegistry(ctx, reg))
		got, err = f.registries.GetRegistry(ctx, orgA, reg.ID())
		require.NoError(t, err)
		assert.Equal(t, reg.Snapshot(), got.Snapshot())
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, f.registries.UpdateRegistry(ctx, newRegistry(orgA, wsA, "never-stored")), delivery.ErrNotFound)
	})

	t.Run("slugs are unique among active registries", func(t *testing.T) {
		first := f.createRegistry(t, orgA, wsA, "clash")
		assert.ErrorIs(t, f.registries.CreateRegistry(ctx, newRegistry(orgA, wsA, "clash")), delivery.ErrSlugTaken)
		f.createRegistry(t, orgA, wsB, "clash")

		first.Archive()
		require.NoError(t, f.registries.UpdateRegistry(ctx, first))
		second := f.createRegistry(t, orgA, wsA, "clash")
		got, err := f.registries.GetRegistryBySlug(ctx, orgA, wsA, "clash")
		require.NoError(t, err)
		assert.Equal(t, second.ID(), got.ID())

		first.Restore()
		assert.ErrorIs(t, f.registries.UpdateRegistry(ctx, first), delivery.ErrSlugTaken)
	})
}

func TestRegistryListAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	first := f.createRegistry(t, orgA, wsA, "first")
	at(t, time.Second)
	archived := f.createRegistry(t, orgA, wsA, "archived")
	archived.Archive()
	require.NoError(t, f.registries.UpdateRegistry(ctx, archived))
	f.createRegistry(t, orgA, wsB, "elsewhere")
	f.createRegistry(t, orgB, wsA, "other-org")

	page, err := f.registries.ListRegistries(ctx, delivery.ListRegistriesQuery{OrganizationID: orgA, WorkspaceID: wsA})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID()}, idsOf(page.Items))
	assert.Equal(t, first.Snapshot(), page.Items[0].Snapshot())
	assert.Equal(t, 1, page.TotalCount)

	page, err = f.registries.ListRegistries(ctx, delivery.ListRegistriesQuery{OrganizationID: orgA, WorkspaceID: wsA, IncludeArchived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID(), archived.ID()}, idsOf(page.Items))

	n, err := f.registries.CountActiveRegistries(ctx, orgA, wsA)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

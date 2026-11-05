package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

func TestOrganizationRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	t.Run("round trip by ID and slug", func(t *testing.T) {
		org := must(iam.NewOrganization(iam.NewOrganizationProps{
			Slug:        "acme",
			Name:        "Acme Corporation Ltd",
			DisplayName: "Acme",
			LogoURL:     new("https://acme.example.com/logo.png"),
			Metadata:    map[string]string{"plan": "team"},
		}, nil))
		require.NoError(t, f.orgs.Write(ctx, org))

		byID, err := f.orgs.GetByIdentifier(ctx, org.ID())
		require.NoError(t, err)
		assert.Equal(t, org.Snapshot(), byID.Snapshot())

		bySlug, err := f.orgs.GetByIdentifier(ctx, "acme")
		require.NoError(t, err)
		assert.Equal(t, org.Snapshot(), bySlug.Snapshot())
	})

	t.Run("round trip with defaults", func(t *testing.T) {
		org := f.writeOrg(t, "plain")

		got, err := f.orgs.GetByIdentifier(ctx, org.ID())
		require.NoError(t, err)
		assert.Equal(t, org.Snapshot(), got.Snapshot())
		assert.Nil(t, got.LogoURL())
		assert.Nil(t, got.Metadata())
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.organizations WHERE id = ? AND metadata IS NULL", org.ID()), "no metadata is NULL, not a JSON null")
	})

	t.Run("unknown identifier", func(t *testing.T) {
		_, err := f.orgs.GetByIdentifier(ctx, "nobody")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("get several", func(t *testing.T) {
		at(t, time.Minute)
		first := f.writeOrg(t, "first")
		at(t, 2*time.Minute)
		second := f.writeOrg(t, "second")
		f.writeOrg(t, "unasked")

		got, err := f.orgs.GetByIdentifiers(ctx, []string{"second", first.ID(), "nobody"})
		require.NoError(t, err)
		assert.Equal(t, []string{first.ID(), second.ID()}, idsOf(got), "oldest first, unknown identifiers left out")

		got, err = f.orgs.GetByIdentifiers(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("exists", func(t *testing.T) {
		f.writeOrg(t, "exists")

		byName, err := f.orgs.ExistsByName(ctx, "ORG EXISTS")
		require.NoError(t, err)
		assert.True(t, byName, "names match ignoring case")
		bySlug, err := f.orgs.ExistsBySlug(ctx, "exists")
		require.NoError(t, err)
		assert.True(t, bySlug)

		byName, err = f.orgs.ExistsByName(ctx, "Org Missing")
		require.NoError(t, err)
		assert.False(t, byName)
		bySlug, err = f.orgs.ExistsBySlug(ctx, "missing")
		require.NoError(t, err)
		assert.False(t, bySlug)
	})

	t.Run("write updates a stored organization", func(t *testing.T) {
		org := f.writeOrg(t, "before")
		stored := f.loadOrg(t, org.ID())
		at(t, time.Hour)
		require.NoError(t, stored.Update(iam.UpdateOrganizationProps{Slug: new("after"), DisplayName: new("After"), Metadata: map[string]string{"tier": "gold"}}))
		require.NoError(t, stored.Suspend())

		require.NoError(t, f.orgs.Write(ctx, stored))

		got := f.loadOrg(t, org.ID())
		assert.Equal(t, stored.Snapshot(), got.Snapshot())
		assert.Equal(t, org.CreatedAt(), got.CreatedAt(), "creation time never changes")
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.organizations WHERE id = ?", org.ID()))
	})

	t.Run("slug held by another organization", func(t *testing.T) {
		f.writeOrg(t, "taken")

		err := f.orgs.Write(ctx, newOrg("taken"))

		assert.ErrorIs(t, err, iam.ErrAlreadyExists)
	})
}

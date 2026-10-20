package delivery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func domRegistryProps() NewRegistryProps {
	return NewRegistryProps{
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "ghcr",
		DisplayName:    "GHCR",
		Kind:           RegistryOCI,
		URL:            "https://ghcr.io/acme",
	}
}

func domNewRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(domRegistryProps(), nil)
	require.NoError(t, err)
	return r
}

func TestNewRegistry(t *testing.T) {
	clock.Fix(t, domT0)
	props := domRegistryProps()
	props.CredentialRef = new("cred_ghcr")

	r, err := NewRegistry(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("reg", r.ID()), "a new registry gets a reg_ ID: %q", r.ID())
	assert.Equal(t, domOrg, r.OrganizationID())
	assert.Equal(t, domWorkspace, r.WorkspaceID())
	assert.Equal(t, "ghcr", r.Slug())
	assert.Equal(t, "GHCR", r.DisplayName())
	assert.Equal(t, RegistryOCI, r.Kind())
	assert.Equal(t, "https://ghcr.io/acme", r.URL())
	assert.Equal(t, "cred_ghcr", *r.CredentialRef())
	assert.Equal(t, LookupFromControlPlane, r.LookupFrom(), "lookups default to the control plane")
	assert.Nil(t, r.LookupPool())
	assert.Equal(t, domT0, r.CreatedAt())
	assert.Equal(t, domT0, r.UpdatedAt())
	assert.Nil(t, r.DeletedAt())
	assert.False(t, r.Archived())
}

func TestNewRegistryOptions(t *testing.T) {
	t.Run("slug derived from display name", func(t *testing.T) {
		props := domRegistryProps()
		props.Slug = ""
		props.DisplayName = "Acme Harbor"
		r, err := NewRegistry(props, nil)
		require.NoError(t, err)
		assert.Equal(t, "acme-harbor", r.Slug())
	})

	t.Run("empty credential and pool are none", func(t *testing.T) {
		props := domRegistryProps()
		props.CredentialRef = new("")
		props.LookupPool = new("")
		r, err := NewRegistry(props, nil)
		require.NoError(t, err)
		assert.Nil(t, r.CredentialRef())
		assert.Nil(t, r.LookupPool())
	})

	t.Run("data plane lookups with a pool", func(t *testing.T) {
		props := domRegistryProps()
		props.LookupFrom = LookupFromDataPlane
		props.LookupPool = new("eu-pool")
		r, err := NewRegistry(props, nil)
		require.NoError(t, err)
		assert.Equal(t, LookupFromDataPlane, r.LookupFrom())
		assert.Equal(t, "eu-pool", *r.LookupPool())
	})

	t.Run("http and ports are allowed", func(t *testing.T) {
		props := domRegistryProps()
		props.URL = "http://registry.internal:5000"
		r, err := NewRegistry(props, nil)
		require.NoError(t, err)
		assert.Equal(t, "http://registry.internal:5000", r.URL())
	})
}

func TestNewRegistryWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("reg")
	props := domRegistryProps()
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.DeletedAt = new(domT1)

	r, err := NewRegistry(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, r.ID())
	assert.Equal(t, domT0, r.CreatedAt())
	assert.Equal(t, domT1, r.UpdatedAt())
	assert.Equal(t, domT1, *r.DeletedAt())
	assert.True(t, r.Archived())
}

func TestNewRegistryIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewRegistryProps)
		id     *string
	}{
		{"no organization", func(p *NewRegistryProps) { p.OrganizationID = "" }, nil},
		{"no workspace", func(p *NewRegistryProps) { p.WorkspaceID = "" }, nil},
		{"ID with wrong prefix", func(*NewRegistryProps) {}, new(id.New("art"))},
		{"malformed ID", func(*NewRegistryProps) {}, new("reg_01ARZ3NDEKTSV4RRFFQ69G5FA")},
		{"no display name", func(p *NewRegistryProps) { p.DisplayName = "" }, nil},
		{"bad slug", func(p *NewRegistryProps) { p.Slug = "ghcr." }, nil},
		{"no kind", func(p *NewRegistryProps) { p.Kind = "" }, nil},
		{"unsupported kind", func(p *NewRegistryProps) { p.Kind = "maven" }, nil},
		{"no URL", func(p *NewRegistryProps) { p.URL = "" }, nil},
		{"relative URL", func(p *NewRegistryProps) { p.URL = "ghcr.io/acme" }, nil},
		{"unsupported scheme", func(p *NewRegistryProps) { p.URL = "oci://ghcr.io/acme" }, nil},
		{"no host", func(p *NewRegistryProps) { p.URL = "https:///acme" }, nil},
		{"unparseable URL", func(p *NewRegistryProps) { p.URL = "https://ghcr.io/%zz" }, nil},
		{"user in URL", func(p *NewRegistryProps) { p.URL = "https://bot@ghcr.io/acme" }, nil},
		{"password in URL", func(p *NewRegistryProps) { p.URL = "https://bot:s3cret@ghcr.io/acme" }, nil},
		{"query in URL", func(p *NewRegistryProps) { p.URL = "https://ghcr.io/acme?token=x" }, nil},
		{"fragment in URL", func(p *NewRegistryProps) { p.URL = "https://ghcr.io/acme#top" }, nil},
		{"unknown lookup_from", func(p *NewRegistryProps) { p.LookupFrom = "edge" }, nil},
		{"control plane with a pool", func(p *NewRegistryProps) { p.LookupPool = new("eu-pool") }, nil},
		{"data plane without a pool", func(p *NewRegistryProps) { p.LookupFrom = LookupFromDataPlane }, nil},
		{"data plane with an empty pool", func(p *NewRegistryProps) { p.LookupFrom = LookupFromDataPlane; p.LookupPool = new("") }, nil},
		{"data plane with a pool too long", func(p *NewRegistryProps) {
			p.LookupFrom = LookupFromDataPlane
			p.LookupPool = new(strings.Repeat("p", maxPoolLength+1))
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domRegistryProps()
			tt.mutate(&props)
			_, err := NewRegistry(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestRegistryGettersReturnCopies(t *testing.T) {
	props := domRegistryProps()
	props.CredentialRef = new("cred_ghcr")
	props.LookupFrom = LookupFromDataPlane
	props.LookupPool = new("eu-pool")
	props.DeletedAt = new(domT0)
	r, err := NewRegistry(props, nil)
	require.NoError(t, err)
	before := r.Snapshot()

	*props.CredentialRef = "changed"
	*props.LookupPool = "changed"
	*props.DeletedAt = domT2
	*r.CredentialRef() = "changed"
	*r.LookupPool() = "changed"
	*r.DeletedAt() = domT2
	snap := r.Snapshot()
	*snap.CredentialRef = "changed"
	*snap.LookupPool = "changed"

	assert.Equal(t, before, r.Snapshot())
}

func TestRegistrySnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	r := domNewRegistry(t)

	assert.Equal(t, RegistrySnapshot{
		ID:             r.ID(),
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "ghcr",
		DisplayName:    "GHCR",
		Kind:           RegistryOCI,
		URL:            "https://ghcr.io/acme",
		LookupFrom:     LookupFromControlPlane,
		CreatedAt:      domT0,
		UpdatedAt:      domT0,
	}, r.Snapshot())
}

func TestRegistryUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	r := domNewRegistry(t)
	clock.Fix(t, domT1)

	require.NoError(t, r.Update(UpdateRegistryProps{Slug: new("harbor"), DisplayName: new("Harbor"), URL: new("https://harbor.acme.dev")}))

	assert.Equal(t, "harbor", r.Slug())
	assert.Equal(t, "Harbor", r.DisplayName())
	assert.Equal(t, "https://harbor.acme.dev", r.URL())
	assert.Equal(t, domT1, r.UpdatedAt())

	t.Run("unset fields are kept", func(t *testing.T) {
		require.NoError(t, r.Update(UpdateRegistryProps{DisplayName: new("Harbor 2")}))
		assert.Equal(t, "harbor", r.Slug())
		assert.Equal(t, "https://harbor.acme.dev", r.URL())
	})
}

func TestRegistryUpdateIsAtomic(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateRegistryProps
	}{
		{"bad slug", UpdateRegistryProps{Slug: new("Harbor"), DisplayName: new("Harbor"), URL: new("https://harbor.acme.dev")}},
		{"bad display name", UpdateRegistryProps{Slug: new("harbor"), DisplayName: new(""), URL: new("https://harbor.acme.dev")}},
		{"bad URL", UpdateRegistryProps{Slug: new("harbor"), DisplayName: new("Harbor"), URL: new("ftp://harbor.acme.dev")}},
		{"credentials in URL", UpdateRegistryProps{Slug: new("harbor"), DisplayName: new("Harbor"), URL: new("https://u:p@harbor.acme.dev")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			r := domNewRegistry(t)
			before := r.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, r.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, r.Snapshot())
		})
	}
}

func TestRegistryRefusesChangesWhenArchived(t *testing.T) {
	r := domNewRegistry(t)
	r.Archive()
	before := r.Snapshot()

	assert.ErrorIs(t, r.Update(UpdateRegistryProps{DisplayName: new("New")}), ErrArchived)
	assert.ErrorIs(t, r.SetCredential("cred_x"), ErrArchived)
	assert.ErrorIs(t, r.ClearCredential(), ErrArchived)
	assert.ErrorIs(t, r.SetLookup(LookupFromDataPlane, "eu-pool"), ErrArchived)
	assert.Equal(t, before, r.Snapshot())
}

func TestRegistryCredential(t *testing.T) {
	clock.Fix(t, domT0)
	r := domNewRegistry(t)

	clock.Fix(t, domT1)
	require.NoError(t, r.SetCredential("cred_ghcr"))
	assert.Equal(t, "cred_ghcr", *r.CredentialRef())
	assert.Equal(t, domT1, r.UpdatedAt())

	t.Run("an empty reference is refused", func(t *testing.T) {
		before := r.Snapshot()
		assert.ErrorIs(t, r.SetCredential(""), ErrInvalid)
		assert.Equal(t, before, r.Snapshot())
	})

	clock.Fix(t, domT2)
	require.NoError(t, r.ClearCredential())
	assert.Nil(t, r.CredentialRef())
	assert.Equal(t, domT2, r.UpdatedAt())
}

func TestRegistrySetLookup(t *testing.T) {
	clock.Fix(t, domT0)
	r := domNewRegistry(t)

	clock.Fix(t, domT1)
	require.NoError(t, r.SetLookup(LookupFromDataPlane, "eu-pool"))
	assert.Equal(t, LookupFromDataPlane, r.LookupFrom())
	assert.Equal(t, "eu-pool", *r.LookupPool())
	assert.Equal(t, domT1, r.UpdatedAt())

	clock.Fix(t, domT2)
	require.NoError(t, r.SetLookup(LookupFromControlPlane, ""))
	assert.Equal(t, LookupFromControlPlane, r.LookupFrom())
	assert.Nil(t, r.LookupPool(), "an empty pool is none")
	assert.Equal(t, domT2, r.UpdatedAt())

	tests := []struct {
		name string
		from LookupFrom
		pool string
	}{
		{"data plane without a pool", LookupFromDataPlane, ""},
		{"data plane with a pool too long", LookupFromDataPlane, strings.Repeat("é", maxPoolLength+1)},
		{"control plane with a pool", LookupFromControlPlane, "eu-pool"},
		{"empty lookup_from", "", ""},
		{"unknown lookup_from", "edge", "eu-pool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := r.Snapshot()
			assert.ErrorIs(t, r.SetLookup(tt.from, tt.pool), ErrInvalid)
			assert.Equal(t, before, r.Snapshot())
		})
	}

	t.Run("pool limit counts characters", func(t *testing.T) {
		require.NoError(t, r.SetLookup(LookupFromDataPlane, strings.Repeat("é", maxPoolLength)))
	})
}

func TestRegistryArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	r := domNewRegistry(t)

	clock.Fix(t, domT1)
	r.Archive()
	assert.True(t, r.Archived())
	assert.Equal(t, domT1, *r.DeletedAt())
	assert.Equal(t, domT1, r.UpdatedAt())

	clock.Fix(t, domT2)
	r.Archive()
	assert.Equal(t, domT1, *r.DeletedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, r.UpdatedAt())

	r.Restore()
	assert.False(t, r.Archived())
	assert.Nil(t, r.DeletedAt())
	assert.Equal(t, domT2, r.UpdatedAt())

	clock.Fix(t, domT0)
	r.Restore()
	assert.Equal(t, domT2, r.UpdatedAt(), "restoring an active registry does nothing")
}

func TestRegistryURLNeedsAHostName(t *testing.T) {

	props := domRegistryProps()
	props.URL = "https://:443"
	_, err := NewRegistry(props, nil)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestRegistryURLRefusesEmptyQueryAndFragment(t *testing.T) {

	for _, raw := range []string{"https://ghcr.io/acme?", "https://ghcr.io/acme#"} {
		t.Run(raw, func(t *testing.T) {
			props := domRegistryProps()
			props.URL = raw
			_, err := NewRegistry(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

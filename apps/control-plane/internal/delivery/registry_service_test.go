package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestServiceCreateRegistry(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		f := newSvcFixture(t)

		r, err := f.svc.CreateRegistry(svcCtx, svcAlice, CreateRegistryInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "GitHub", Kind: RegistryOCI, URL: "https://ghcr.io", CredentialRef: "crd_1",
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Registry{}.Prefix(), r.ID()))
		assert.Equal(t, LookupFromControlPlane, r.LookupFrom(), "lookups default to the control plane")
		assert.Nil(t, r.LookupPool())
		assert.Equal(t, "crd_1", *r.CredentialRef())
		ev := f.audit.last()
		assert.Equal(t, EventRegistryCreated, ev.Type)
		assert.Equal(t, r.Snapshot(), ev.After)
		stored, err := f.store.GetRegistry(svcCtx, svcOrg, r.ID())
		require.NoError(t, err)
		assert.Equal(t, r.Snapshot(), stored.Snapshot())
	})

	t.Run("anonymous from the data plane", func(t *testing.T) {
		f := newSvcFixture(t)
		r, err := f.svc.CreateRegistry(svcCtx, svcAlice, CreateRegistryInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "Internal", Kind: RegistryOCI, URL: "https://registry.internal",
			LookupFrom: LookupFromDataPlane, LookupPool: "pool-a",
		})
		require.NoError(t, err)
		assert.Nil(t, r.CredentialRef())
		assert.Equal(t, "pool-a", *r.LookupPool())
	})

	t.Run("refused", func(t *testing.T) {
		f := newSvcFixture(t)
		f.registry(t, "ghcr")
		archived, _ := f.addWorkspace(t, "old", "legacy")
		f.archiveWorkspace(t, archived)

		tests := []struct {
			name    string
			in      CreateRegistryInput
			wantErr error
		}{
			{"slug taken", CreateRegistryInput{WorkspaceID: f.ws.ID(), Slug: "ghcr", DisplayName: "G", Kind: RegistryOCI, URL: "https://ghcr.io"}, ErrSlugTaken},
			{"archived workspace", CreateRegistryInput{WorkspaceID: archived.ID(), DisplayName: "G", Kind: RegistryOCI, URL: "https://ghcr.io"}, ErrArchived},
			{"missing workspace", CreateRegistryInput{WorkspaceID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAW", DisplayName: "G", Kind: RegistryOCI, URL: "https://ghcr.io"}, ErrNotFound},
			{"data plane without a pool", CreateRegistryInput{WorkspaceID: f.ws.ID(), DisplayName: "G", Kind: RegistryOCI, URL: "https://ghcr.io", LookupFrom: LookupFromDataPlane}, ErrInvalid},
			{"a pool for control plane lookups", CreateRegistryInput{WorkspaceID: f.ws.ID(), DisplayName: "G", Kind: RegistryOCI, URL: "https://ghcr.io", LookupPool: "p"}, ErrInvalid},
			{"no host", CreateRegistryInput{WorkspaceID: f.ws.ID(), DisplayName: "G", Kind: RegistryOCI, URL: "https://:443"}, ErrInvalid},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				state, events, resources := f.marks()
				tt.in.OrganizationID = svcOrg
				_, err := f.svc.CreateRegistry(svcCtx, svcAlice, tt.in)
				require.ErrorIs(t, err, tt.wantErr)
				f.requireUnchanged(t, state, events, resources)
			})
		}
	})
}

func TestServiceChangeRegistry(t *testing.T) {
	f := newSvcFixture(t)
	r := f.registry(t, "ghcr")
	f.registry(t, "ecr")

	t.Run("get and list", func(t *testing.T) {
		got, err := f.svc.GetRegistry(svcCtx, svcAlice, svcOrg, r.ID())
		require.NoError(t, err)
		assert.Equal(t, r.Snapshot(), got.Snapshot())
		_, err = f.svc.GetRegistry(svcCtx, svcAlice, svcOrg, "reg_01ARZ3NDEKTSV4RRFFQ69G5FAW")
		require.ErrorIs(t, err, ErrNotFound)
		page, err := f.svc.ListRegistries(svcCtx, svcAlice, ListRegistriesQuery{OrganizationID: svcOrg, WorkspaceID: f.ws.ID()})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})

	t.Run("update", func(t *testing.T) {
		got, err := f.svc.UpdateRegistry(svcCtx, svcAlice, svcOrg, r.ID(), UpdateRegistryProps{URL: new("https://ghcr.example.com")})
		require.NoError(t, err)
		assert.Equal(t, "https://ghcr.example.com", got.URL())
		ev := f.audit.last()
		assert.Equal(t, EventRegistryUpdated, ev.Type)
		assert.Equal(t, "https://ghcr.io", ev.Before.(RegistrySnapshot).URL)

		_, err = f.svc.UpdateRegistry(svcCtx, svcAlice, svcOrg, r.ID(), UpdateRegistryProps{Slug: new("ecr")})
		require.ErrorIs(t, err, ErrSlugTaken)
	})

	t.Run("credential", func(t *testing.T) {
		got, err := f.svc.SetRegistryCredential(svcCtx, svcAlice, svcOrg, r.ID(), "crd_2")
		require.NoError(t, err)
		assert.Equal(t, "crd_2", *got.CredentialRef())
		assert.Equal(t, EventRegistryUpdated, f.audit.last().Type)

		got, err = f.svc.SetRegistryCredential(svcCtx, svcAlice, svcOrg, r.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.CredentialRef(), "an empty reference makes the registry anonymous")
		stored, err := f.store.GetRegistry(svcCtx, svcOrg, r.ID())
		require.NoError(t, err)
		assert.Nil(t, stored.CredentialRef())
	})

	t.Run("lookup", func(t *testing.T) {
		got, err := f.svc.SetRegistryLookup(svcCtx, svcAlice, svcOrg, r.ID(), LookupFromDataPlane, "pool-a")
		require.NoError(t, err)
		assert.Equal(t, LookupFromDataPlane, got.LookupFrom())
		assert.Equal(t, "pool-a", *got.LookupPool())

		_, err = f.svc.SetRegistryLookup(svcCtx, svcAlice, svcOrg, r.ID(), LookupFromControlPlane, "pool-a")
		require.ErrorIs(t, err, ErrInvalid)

		got, err = f.svc.SetRegistryLookup(svcCtx, svcAlice, svcOrg, r.ID(), LookupFromControlPlane, "")
		require.NoError(t, err)
		assert.Nil(t, got.LookupPool())
	})

	t.Run("archive", func(t *testing.T) {
		got, err := f.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, r.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, EventRegistryArchived, f.audit.last().Type)

		_, err = f.svc.SetRegistryCredential(svcCtx, svcAlice, svcOrg, r.ID(), "crd_3")
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("restore", func(t *testing.T) {
		got, err := f.svc.RestoreRegistry(svcCtx, svcAlice, svcOrg, r.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, EventRegistryRestored, f.audit.last().Type)
	})

	t.Run("restore is refused in an archived workspace", func(t *testing.T) {
		_, err := f.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, r.ID())
		require.NoError(t, err)
		f.archiveWorkspace(t, f.ws)
		state, events, resources := f.marks()

		_, err = f.svc.RestoreRegistry(svcCtx, svcAlice, svcOrg, r.ID())

		require.ErrorIs(t, err, ErrArchived)
		f.requireUnchanged(t, state, events, resources)
	})
}

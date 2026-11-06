package registries

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

func ptr[T any](v T) *T { return &v }

func ghcr(workspaceID string) delivery.CreateRegistryInput {
	return delivery.CreateRegistryInput{OrganizationID: org, WorkspaceID: workspaceID, Slug: "ghcr", DisplayName: "GHCR", Kind: delivery.RegistryOCI, URL: "https://ghcr.io"}
}

func TestNewRegistryServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]RegistryServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewRegistryService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewRegistryService(all...)
	assert.NoError(t, err)
}

func TestCreateRegistry(t *testing.T) {
	ctx := context.Background()

	t.Run("created", func(t *testing.T) {
		w := newWorld(t)

		r, err := w.svc.CreateRegistry(ctx, ada, delivery.CreateRegistryInput{
			OrganizationID: org, WorkspaceID: wsActive, DisplayName: "GitHub", Kind: delivery.RegistryOCI, URL: "https://ghcr.io", CredentialRef: "crd_1",
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(delivery.Registry{}.Prefix(), r.ID()))
		assert.Equal(t, "github", r.Slug(), "slug derived from the display name")
		assert.Equal(t, delivery.LookupFromControlPlane, r.LookupFrom(), "lookups default to the control plane")
		assert.Nil(t, r.LookupPool())
		assert.Equal(t, "crd_1", *r.CredentialRef())
		assert.Equal(t, r.Snapshot(), w.registries[r.ID()])
		require.Len(t, w.audit, 1)
		assert.Equal(t, delivery.EventRegistryCreated, w.audit[0].Type)
		assert.Equal(t, ada, w.audit[0].Actor)
		assert.Equal(t, r.Snapshot(), w.audit[0].After)
		assert.Equal(t, "dec-"+wsActive, w.audit[0].DecisionID)
	})

	t.Run("anonymous from the data plane", func(t *testing.T) {
		w := newWorld(t)
		r, err := w.svc.CreateRegistry(ctx, ada, delivery.CreateRegistryInput{
			OrganizationID: org, WorkspaceID: wsActive, DisplayName: "Internal", Kind: delivery.RegistryOCI, URL: "https://registry.internal",
			LookupFrom: delivery.LookupFromDataPlane, LookupPool: "pool-a",
		})
		require.NoError(t, err)
		assert.Nil(t, r.CredentialRef())
		assert.Equal(t, "pool-a", *r.LookupPool())
	})

	refusals := []struct {
		name    string
		in      delivery.CreateRegistryInput
		wantErr error
	}{
		{"slug taken", delivery.CreateRegistryInput{WorkspaceID: wsActive, Slug: "taken", DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://ghcr.io"}, delivery.ErrSlugTaken},
		{"archived workspace", delivery.CreateRegistryInput{WorkspaceID: wsArchived, DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://ghcr.io"}, delivery.ErrArchived},
		{"missing workspace", delivery.CreateRegistryInput{WorkspaceID: "wsp_01JZ0000000000000000000099", DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://ghcr.io"}, delivery.ErrNotFound},
		{"data plane without a pool", delivery.CreateRegistryInput{WorkspaceID: wsActive, DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://ghcr.io", LookupFrom: delivery.LookupFromDataPlane}, delivery.ErrInvalid},
		{"a pool for control plane lookups", delivery.CreateRegistryInput{WorkspaceID: wsActive, DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://ghcr.io", LookupPool: "p"}, delivery.ErrInvalid},
		{"no host", delivery.CreateRegistryInput{WorkspaceID: wsActive, DisplayName: "G", Kind: delivery.RegistryOCI, URL: "https://:443"}, delivery.ErrInvalid},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.registry(t, wsActive, "taken", false)
			before := w.state.clone()
			tt.in.OrganizationID = org

			_, err := w.svc.CreateRegistry(ctx, ada, tt.in)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, w.state)
		})
	}
}

// Every operation asks the authorizer the right question, about the
// Registry's Workspace, before it does anything.
func TestOperationsAreAuthorizedOnTheWorkspace(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	r := w.registry(t, wsActive, "ghcr", false)

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
		proposal   any
	}{
		{"create", func() error {
			_, err := w.svc.CreateRegistry(ctx, ada, delivery.CreateRegistryInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "ECR", Kind: delivery.RegistryOCI, URL: "https://ecr.example.com"})
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistryCreate,
			delivery.CreateRegistryInput{OrganizationID: org, WorkspaceID: wsActive, DisplayName: "ECR", Kind: delivery.RegistryOCI, URL: "https://ecr.example.com"}},
		{"get", func() error {
			_, err := w.svc.GetRegistry(ctx, ada, org, r.ID())
			return err
		}, delivery.PermissionRegistryRead, delivery.ActionRegistryRead, nil},
		{"list", func() error {
			_, err := w.svc.ListRegistries(ctx, ada, delivery.ListRegistriesQuery{OrganizationID: org, WorkspaceID: wsActive})
			return err
		}, delivery.PermissionRegistryRead, delivery.ActionRegistryList, nil},
		{"update", func() error {
			_, err := w.svc.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{DisplayName: ptr("GHCR")})
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistryUpdate, delivery.UpdateRegistryProps{DisplayName: ptr("GHCR")}},
		{"set credential", func() error {
			_, err := w.svc.SetRegistryCredential(ctx, ada, org, r.ID(), "crd_1")
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistrySetCredential, "crd_1"},
		{"set lookup", func() error {
			_, err := w.svc.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromDataPlane, "pool-a")
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistrySetLookup, struct {
			LookupFrom delivery.LookupFrom
			LookupPool string
		}{delivery.LookupFromDataPlane, "pool-a"}},
		{"archive", func() error {
			_, err := w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistryArchive, nil},
		{"restore", func() error {
			_, err := w.svc.RestoreRegistry(ctx, ada, org, r.ID())
			return err
		}, delivery.PermissionRegistryManage, delivery.ActionRegistryRestore, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.reset()
			require.NoError(t, tc.call())
			require.Len(t, w.authz.asked, 1)
			req := w.authz.asked[0]
			assert.Equal(t, ada, req.Actor)
			assert.Equal(t, org, req.OrganizationID)
			assert.Equal(t, tc.permission, req.Permission)
			assert.Equal(t, tc.action, req.Action)
			assert.Equal(t, wsActive, req.ResourceID)
			assert.Equal(t, tc.proposal, req.Proposal)
		})
	}
}

// A refused change writes nothing, whether it is denied or needs approval.
func TestRefusedChangesWriteNothing(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(w *world, r, old *delivery.Registry) error{
		"create": func(w *world, _, _ *delivery.Registry) error {
			_, err := w.svc.CreateRegistry(ctx, ada, ghcr(wsOther))
			return err
		},
		"update": func(w *world, r, _ *delivery.Registry) error {
			_, err := w.svc.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{DisplayName: ptr("Renamed")})
			return err
		},
		"set credential": func(w *world, r, _ *delivery.Registry) error {
			_, err := w.svc.SetRegistryCredential(ctx, ada, org, r.ID(), "crd_1")
			return err
		},
		"set lookup": func(w *world, r, _ *delivery.Registry) error {
			_, err := w.svc.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromDataPlane, "pool-a")
			return err
		},
		"archive": func(w *world, r, _ *delivery.Registry) error {
			_, err := w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
			return err
		},
		"restore": func(w *world, _, old *delivery.Registry) error {
			_, err := w.svc.RestoreRegistry(ctx, ada, org, old.ID())
			return err
		},
	}
	outcomes := map[authorization.Outcome]error{
		authorization.OutcomeDeny:             authorization.ErrDenied,
		authorization.OutcomeRequiresApproval: authorization.ErrApprovalRequired,
	}
	for outcome, wantErr := range outcomes {
		for name, call := range cases {
			t.Run(string(outcome)+"/"+name, func(t *testing.T) {
				w := newWorld(t)
				r := w.registry(t, wsActive, "ghcr", false)
				old := w.registry(t, wsActive, "old", true)
				w.authz.outcome = outcome
				before := w.state.clone()

				err := call(w, r, old)

				require.ErrorIs(t, err, wantErr)
				assert.Equal(t, before, w.state)
			})
		}
	}
}

// The service under the wrapper refuses to change anything without a
// decision, so wiring it up without authorization fails loudly.
func TestServiceWithoutAuthorizationChangesNothing(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	r := w.registry(t, wsActive, "ghcr", false)
	old := w.registry(t, wsActive, "old", true)
	core := w.svc.(*authorized).next
	before := w.state.clone()

	_, err := core.CreateRegistry(ctx, ada, ghcr(wsOther))
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{DisplayName: ptr("Renamed")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.SetRegistryCredential(ctx, ada, org, r.ID(), "crd_1")
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromDataPlane, "pool-a")
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.ArchiveRegistry(ctx, ada, org, r.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	_, err = core.RestoreRegistry(ctx, ada, org, old.ID())
	assert.ErrorIs(t, err, change.ErrUnauthorized)

	assert.Equal(t, before, w.state)
}

func TestAuditRecordsCarryTheDecision(t *testing.T) {
	w := newWorld(t)
	w.authz.outcome = authorization.OutcomeAllow
	r := w.registry(t, wsActive, "ghcr", false)

	_, err := w.svc.UpdateRegistry(context.Background(), ada, org, r.ID(), delivery.UpdateRegistryProps{DisplayName: ptr("GitHub")})

	require.NoError(t, err)
	require.Len(t, w.audit, 1)
	assert.Equal(t, "dec-"+wsActive, w.audit[0].DecisionID)
}

func TestReadRegistries(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	r := w.registry(t, wsActive, "ghcr", false)
	w.registry(t, wsActive, "ecr", false)

	got, err := w.svc.GetRegistry(ctx, ada, org, r.ID())
	require.NoError(t, err)
	assert.Equal(t, r.Snapshot(), got.Snapshot())

	_, err = w.svc.GetRegistry(ctx, ada, org, "reg_01JZ0000000000000000000099")
	require.ErrorIs(t, err, delivery.ErrNotFound)

	page, err := w.svc.ListRegistries(ctx, ada, delivery.ListRegistriesQuery{OrganizationID: org, WorkspaceID: wsActive})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)

	t.Run("a refused read returns nothing", func(t *testing.T) {
		w.authz.deny = func(authorization.Request) bool { return true }
		t.Cleanup(func() { w.authz.deny = nil })
		got, err := w.svc.GetRegistry(ctx, ada, org, r.ID())
		require.ErrorIs(t, err, authorization.ErrDenied)
		assert.Nil(t, got)
	})
}

func TestChangeRegistry(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	r := w.registry(t, wsActive, "ghcr", false)
	w.registry(t, wsActive, "ecr", false)

	t.Run("update", func(t *testing.T) {
		got, err := w.svc.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{URL: ptr("https://ghcr.example.com")})
		require.NoError(t, err)
		assert.Equal(t, "https://ghcr.example.com", got.URL())
		ev := w.lastAudit()
		assert.Equal(t, delivery.EventRegistryUpdated, ev.Type)
		assert.Equal(t, "https://ghcr.io", ev.Before.(delivery.RegistrySnapshot).URL)

		_, err = w.svc.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{Slug: ptr("ecr")})
		require.ErrorIs(t, err, delivery.ErrSlugTaken)
	})

	t.Run("credential", func(t *testing.T) {
		got, err := w.svc.SetRegistryCredential(ctx, ada, org, r.ID(), "crd_2")
		require.NoError(t, err)
		assert.Equal(t, "crd_2", *got.CredentialRef())
		assert.Equal(t, delivery.EventRegistryUpdated, w.lastAudit().Type)

		got, err = w.svc.SetRegistryCredential(ctx, ada, org, r.ID(), "")
		require.NoError(t, err)
		assert.Nil(t, got.CredentialRef(), "an empty reference makes the registry anonymous")
		assert.Nil(t, w.registries[r.ID()].CredentialRef)
	})

	t.Run("lookup", func(t *testing.T) {
		got, err := w.svc.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromDataPlane, "pool-a")
		require.NoError(t, err)
		assert.Equal(t, delivery.LookupFromDataPlane, got.LookupFrom())
		assert.Equal(t, "pool-a", *got.LookupPool())

		_, err = w.svc.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromControlPlane, "pool-a")
		require.ErrorIs(t, err, delivery.ErrInvalid)

		got, err = w.svc.SetRegistryLookup(ctx, ada, org, r.ID(), delivery.LookupFromControlPlane, "")
		require.NoError(t, err)
		assert.Nil(t, got.LookupPool())
	})

	t.Run("archive", func(t *testing.T) {
		got, err := w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, delivery.EventRegistryArchived, w.lastAudit().Type)

		w.reset()
		_, err = w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		assert.Empty(t, w.audit, "archiving an archived registry records nothing")

		_, err = w.svc.SetRegistryCredential(ctx, ada, org, r.ID(), "crd_3")
		require.ErrorIs(t, err, delivery.ErrArchived)
	})

	t.Run("restore", func(t *testing.T) {
		got, err := w.svc.RestoreRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		assert.False(t, got.Archived())
		assert.Equal(t, delivery.EventRegistryRestored, w.lastAudit().Type)
	})

	t.Run("restore is refused in an archived workspace", func(t *testing.T) {
		_, err := w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		require.NoError(t, w.workspaces[wsActive].Archive())
		before := w.state.clone()

		_, err = w.svc.RestoreRegistry(ctx, ada, org, r.ID())

		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, w.state)
	})
}

func TestRegistryEventsGoToTheOutbox(t *testing.T) {
	ctx := context.Background()

	t.Run("create and update", func(t *testing.T) {
		w := newWorld(t)
		r, err := w.svc.CreateRegistry(ctx, ada, ghcr(wsActive))
		require.NoError(t, err)

		require.Len(t, w.events, 1)
		created := w.events[0]
		assert.Equal(t, "delivery.v1.registry.created", created.Type)
		assert.Equal(t, delivery.AggregateRegistry, created.AggregateType)
		assert.Equal(t, r.ID(), created.AggregateID)
		assert.Equal(t, org, created.OrganizationID)
		assert.Contains(t, string(created.Payload), `"slug":"ghcr"`)

		_, err = w.svc.UpdateRegistry(ctx, ada, org, r.ID(), delivery.UpdateRegistryProps{DisplayName: ptr("GitHub Container Registry")})
		require.NoError(t, err)
		assert.Equal(t, []string{"delivery.v1.registry.created", "delivery.v1.registry.updated"}, w.eventTypes())
		assert.Equal(t, r.ID(), w.events[1].AggregateID)
	})

	t.Run("an event rolls back with the change that raised it", func(t *testing.T) {
		w := newWorld(t)
		boom := errors.New("boom")
		w.auditErr = boom

		_, err := w.svc.CreateRegistry(ctx, ada, ghcr(wsActive))

		require.ErrorIs(t, err, boom)
		assert.Empty(t, w.events)
		assert.Empty(t, w.registries)
	})

	t.Run("audit records follow the changes", func(t *testing.T) {
		w := newWorld(t)
		r, err := w.svc.CreateRegistry(ctx, ada, ghcr(wsActive))
		require.NoError(t, err)
		_, err = w.svc.ArchiveRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		_, err = w.svc.RestoreRegistry(ctx, ada, org, r.ID())
		require.NoError(t, err)
		assert.Equal(t, []string{delivery.EventRegistryCreated, delivery.EventRegistryArchived, delivery.EventRegistryRestored}, w.auditTypes())
	})
}

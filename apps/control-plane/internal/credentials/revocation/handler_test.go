package revocation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

const org = "org_a"

type world struct {
	store   *credentials.SecretStore
	grants  []*credentials.SecretStoreGrant
	saved   []*credentials.SecretStoreGrant
	audit   []audit.Event
	revoked []string
	fail    error
}

func (w *world) CreateSecretStoreGrants(context.Context, []*credentials.SecretStoreGrant) error {
	return nil
}
func (w *world) ListDeploymentGrants(context.Context, string, string) ([]*credentials.SecretStoreGrant, error) {
	return w.grants, nil
}
func (w *world) UpdateSecretStoreGrant(_ context.Context, g *credentials.SecretStoreGrant) error {
	w.saved = append(w.saved, g)
	return nil
}
func (w *world) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
func (w *world) Record(_ context.Context, e audit.Event) error {
	w.audit = append(w.audit, e)
	return nil
}
func (w *world) Get(context.Context, *credentials.SecretStore) (credentials.MintingCredential, error) {
	return credentials.MintingCredential{Token: "dp.sa.minting"}, nil
}
func (w *world) Put(context.Context, *credentials.SecretStore, credentials.MintingCredential) error {
	return nil
}
func (w *world) Delete(context.Context, string, string) error { return nil }
func (w *world) Mint(context.Context, credentials.MintRequest) (credentials.Minted, error) {
	return credentials.Minted{}, nil
}
func (w *world) Revoke(_ context.Context, r credentials.RevokeRequest) (bool, error) {
	if w.fail != nil {
		return false, w.fail
	}
	w.revoked = append(w.revoked, r.ProviderID)
	return true, nil
}

// stores serves the world's one store.
type stores struct {
	credentials.SecretStoreRepository
	w *world
}

func (s stores) GetSecretStore(context.Context, string, string) (*credentials.SecretStore, error) {
	return s.w.store, nil
}

func newWorld(t *testing.T) (*world, *events.Router) {
	t.Helper()
	clock.Fix(t, time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC))
	store, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
		OrganizationID: org, WorkspaceID: "wsp_a", DisplayName: "Doppler", Kind: secretstore.KindDoppler,
		Settings:       credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "prd"}},
		Access:         credentials.Access{Mode: credentials.AccessBrokered},
		EnvironmentIDs: []string{"env_01BX5ZZKBKACTAV9WEVGEMMVRA"},
	}, nil)
	require.NoError(t, err)
	_, err = store.SetCredential("prn_ada", credentials.MintingCredential{Token: "dp.sa.minting"})
	require.NoError(t, err)
	grant := func(mode credentials.AccessMode, token string, expires time.Duration) *credentials.SecretStoreGrant {
		g, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: org, StoreID: store.ID(), DeploymentID: "dpl_a", Mode: mode, TokenID: token,
			ExpiresAt: clock.Now().Add(expires),
		}, nil)
		require.NoError(t, err)
		return g
	}
	revoked := grant(credentials.AccessBrokered, "slug-revoked", 10*time.Minute)
	revoked.Revoke()
	w := &world{store: store, grants: []*credentials.SecretStoreGrant{
		grant(credentials.AccessBrokered, "slug-live", 10*time.Minute),
		grant(credentials.AccessFederated, "jwt", 5*time.Minute),
		grant(credentials.AccessBrokered, "slug-expired", -time.Minute),
		revoked,
	}}
	h, err := NewHandler(WithGrantRepo(w), WithStoreRepo(stores{w: w}), WithBroker(w, w), WithTransactor(w), WithAuditRecorder(w),
		WithLogger(logging.Nop()))
	require.NoError(t, err)
	var router events.Router
	h.Register(&router)
	return w, &router
}

func statusChanged(t *testing.T, status release.DeploymentStatus) events.Envelope {
	t.Helper()
	e, err := events.Wrap(context.Background(), events.Source{OrganizationID: org, AggregateType: delivery.AggregateDeployment, AggregateID: "dpl_a"},
		delivery.DeploymentStatusChanged{
			Deployment:     delivery.DeploymentSnapshot{ID: "dpl_a", OrganizationID: org, Status: status},
			PreviousStatus: release.DeploymentApplying,
		})
	require.NoError(t, err)
	return e[0]
}

// Once a Deployment leaves applying, each of its brokered grants still live
// is revoked and audited by the system; federated, expired, and revoked
// grants are left alone.
func TestRevokesWhenTheDeploymentLeavesApplying(t *testing.T) {
	w, router := newWorld(t)
	require.NoError(t, router.Handle(context.Background(), statusChanged(t, release.DeploymentApplying)))
	assert.Empty(t, w.revoked, "still applying")

	require.NoError(t, router.Handle(context.Background(), statusChanged(t, release.DeploymentProgressing)))
	assert.Equal(t, []string{"slug-live"}, w.revoked)
	require.Len(t, w.saved, 1)
	assert.True(t, w.saved[0].Revoked())
	require.Len(t, w.audit, 1)
	assert.Equal(t, credentials.EventSecretStoreAccessRevoked, w.audit[0].Type)
	assert.Equal(t, identity.ActorSystem, w.audit[0].Actor.Kind)

	require.NoError(t, router.Handle(context.Background(), statusChanged(t, release.DeploymentHealthy)))
	assert.Equal(t, []string{"slug-live"}, w.revoked, "not revoked twice")
}

// A store that cannot be reached is tried again with the event; one that
// refuses is left to expire.
func TestRevocationFailures(t *testing.T) {
	w, router := newWorld(t)
	w.fail = credentials.ErrStoreUnavailable
	assert.ErrorIs(t, router.Handle(context.Background(), statusChanged(t, release.DeploymentFailed)), credentials.ErrStoreUnavailable)

	w, router = newWorld(t)
	w.fail = credentials.ErrStoreRefused
	assert.NoError(t, router.Handle(context.Background(), statusChanged(t, release.DeploymentFailed)))
	assert.Empty(t, w.saved)
}

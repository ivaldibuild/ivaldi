package grants

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/federation"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi/pkg/binding"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

const org = "org_a"

var agent = identity.Caller{
	OrganizationID: org,
	Actor:          identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorWorkload},
}

type world struct {
	assignment *delivery.DeploymentAssignment
	app        *delivery.Application
	grants     []*credentials.SecretStoreGrant
	audit      []audit.Event
}

func (w *world) GetDeploymentAssignment(_ context.Context, actor identity.Actor, _, _ string) (*delivery.DeploymentAssignment, error) {
	if actor.PrincipalID != agent.Actor.PrincipalID {
		return nil, delivery.ErrNotDeploymentAgent
	}
	return w.assignment, nil
}
func (w *world) GetApplication(context.Context, string, string) (*delivery.Application, error) {
	return w.app, nil
}
func (w *world) CreateSecretStoreGrants(_ context.Context, g []*credentials.SecretStoreGrant) error {
	w.grants = append(w.grants, g...)
	return nil
}
func (w *world) ListDeploymentGrants(context.Context, string, string) ([]*credentials.SecretStoreGrant, error) {
	return w.grants, nil
}
func (w *world) UpdateSecretStoreGrant(context.Context, *credentials.SecretStoreGrant) error {
	return nil
}
func (w *world) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
func (w *world) Record(_ context.Context, e audit.Event) error {
	w.audit = append(w.audit, e)
	return nil
}

func store(id string, mode delivery.StoreAccessMode) delivery.AssignmentSecret {
	return delivery.AssignmentSecret{Name: "S" + id, Store: delivery.StoreSettings{ID: id, Kind: delivery.StoreAWSSecretsManager, Access: delivery.StoreAccess{Mode: mode}}}
}

func newWorld(t *testing.T, withIssuer bool) (*world, Service, token.KeyRing) {
	t.Helper()
	clock.Fix(t, time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	app, err := delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: org, WorkspaceID: "wsp_a", ProjectID: "prj_01BX5ZZKBKACTAV9WEVGEMMVRA", OwnerTeamID: "tem_01BX5ZZKBKACTAV9WEVGEMMVRA", DisplayName: "Payments",
	}, nil)
	require.NoError(t, err)
	w := &world{app: app, assignment: &delivery.DeploymentAssignment{
		Deployment: delivery.DeploymentSnapshot{
			ID: "dpl_a", ApplicationID: app.ID(), EnvironmentID: "env_a", TargetID: "tgt_a", ReleaseID: "rel_a",
			Status: release.DeploymentApplying,
		},
		Target:      delivery.TargetSnapshot{ID: "tgt_a", ClusterID: "cls_a"},
		Environment: delivery.EnvironmentSnapshot{ID: "env_a", WorkspaceID: "wsp_a", Tier: delivery.TierProduction},
		Apply: delivery.ApplyAssignment{Secrets: []delivery.AssignmentSecret{
			store("sst_k8s", delivery.StoreAccessAgent),
			store("sst_aws", delivery.StoreAccessFederated),
			store("sst_aws", delivery.StoreAccessFederated),
			store("sst_vault", delivery.StoreAccessFederated),
		}},
	}}
	keys, err := token.NewEphemeralKeyRing()
	require.NoError(t, err)
	opts := []Option{WithAssignments(w), WithApplications(w), WithGrantRepo(w), WithStoreRepo(stores{}), WithTransactor(w), WithAuditRecorder(w)}
	if withIssuer {
		iss, err := federation.NewIssuer(keys, "https://ivaldi.example.com")
		require.NoError(t, err)
		opts = append(opts, WithIssuer(iss))
	}
	svc, err := NewService(opts...)
	require.NoError(t, err)
	return w, svc, keys
}

// Each federated store the Deployment reads gets one JWT for it; a store in
// agent mode gets none. Each grant is recorded and audited without the JWT.
func TestGrantSignsAJWTPerFederatedStore(t *testing.T) {
	w, svc, keys := newWorld(t, true)
	got, err := svc.Grant(context.Background(), agent, "dpl_a")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"sst_aws", "sst_vault"}, []string{got[0].StoreID, got[1].StoreID})

	jws, err := jose.ParseSigned(got[0].JWT, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	payload, err := jws.Verify(keys.PublicKeys()[0].Key)
	require.NoError(t, err)
	var c federation.Claims
	require.NoError(t, json.Unmarshal(payload, &c))
	assert.Equal(t, "org:org_a:store:sst_aws:env:env_a", c.Subject)
	assert.Equal(t, []string{"ivaldi:sst_aws"}, c.Audience)
	assert.Equal(t, "prj_01BX5ZZKBKACTAV9WEVGEMMVRA", c.Project)
	assert.Equal(t, "cls_a", c.Cluster)
	assert.Equal(t, "production", c.EnvironmentTier)

	require.Len(t, w.grants, 2)
	assert.Equal(t, c.ID, w.grants[0].Snapshot().TokenID, "the record names the JWT by its ID")
	require.Len(t, w.audit, 2)
	for _, e := range w.audit {
		assert.Equal(t, credentials.EventSecretStoreAccessGranted, e.Type)
		assert.Equal(t, "dpl_a", e.ResourceID, "on the deployment")
		assert.NotEmpty(t, e.DecisionID)
		recorded, err := json.Marshal(e.After)
		require.NoError(t, err)
		assert.False(t, strings.Contains(string(recorded), got[0].JWT) || strings.Contains(string(recorded), got[1].JWT), "never the JWT")
	}
}

func TestGrantRefusals(t *testing.T) {
	t.Run("a caller that is not a workload", func(t *testing.T) {
		_, svc, _ := newWorld(t, true)
		_, err := svc.Grant(context.Background(), identity.Caller{OrganizationID: org, Actor: identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}}, "dpl_a")
		assert.ErrorIs(t, err, delivery.ErrNotClusterAgent)
	})
	t.Run("another cluster's agent", func(t *testing.T) {
		_, svc, _ := newWorld(t, true)
		other := agent
		other.Actor.PrincipalID = "prn_other"
		_, err := svc.Grant(context.Background(), other, "dpl_a")
		assert.ErrorIs(t, err, delivery.ErrNotDeploymentAgent)
	})
	t.Run("a deployment that has applied", func(t *testing.T) {
		w, svc, _ := newWorld(t, true)
		w.assignment.Deployment.Status = release.DeploymentProgressing
		_, err := svc.Grant(context.Background(), agent, "dpl_a")
		assert.ErrorIs(t, err, delivery.ErrDeploymentStatus)
		assert.Empty(t, w.grants)
	})
	t.Run("an installation that cannot sign", func(t *testing.T) {
		w, svc, _ := newWorld(t, false)
		_, err := svc.Grant(context.Background(), agent, "dpl_a")
		assert.ErrorIs(t, err, delivery.ErrInvalid)
		assert.Empty(t, w.grants)
	})
	t.Run("a deployment whose stores need no grant", func(t *testing.T) {
		w, svc, _ := newWorld(t, false)
		w.assignment.Apply.Secrets = w.assignment.Apply.Secrets[:1]
		got, err := svc.Grant(context.Background(), agent, "dpl_a")
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.Empty(t, w.audit)
	})
}

// stores is a SecretStoreRepository holding a fixed set of stores.
type stores map[string]*credentials.SecretStore

func (s stores) CreateSecretStore(context.Context, *credentials.SecretStore) error { return nil }
func (s stores) GetSecretStore(_ context.Context, _, id string) (*credentials.SecretStore, error) {
	return s[id], nil
}
func (s stores) GetSecretStoreForUpdate(ctx context.Context, org, id string) (*credentials.SecretStore, error) {
	return s.GetSecretStore(ctx, org, id)
}
func (s stores) GetSecretStores(_ context.Context, _ string, ids []string) ([]*credentials.SecretStore, error) {
	var out []*credentials.SecretStore
	for _, id := range ids {
		if store, ok := s[id]; ok {
			out = append(out, store)
		}
	}
	return out, nil
}
func (s stores) GetSecretStoresForShare(ctx context.Context, org string, ids []string) ([]*credentials.SecretStore, error) {
	return s.GetSecretStores(ctx, org, ids)
}
func (s stores) ListSecretStores(context.Context, string, string, bool) ([]*credentials.SecretStore, error) {
	return nil, nil
}
func (s stores) UpdateSecretStore(context.Context, *credentials.SecretStore) error { return nil }
func (s stores) CountActiveSecretStores(context.Context, string, string) (int, error) {
	return len(s), nil
}

type keeper struct{}

func (keeper) Put(context.Context, *credentials.SecretStore, credentials.MintingCredential) error {
	return nil
}
func (keeper) Get(context.Context, *credentials.SecretStore) (credentials.MintingCredential, error) {
	return credentials.MintingCredential{Token: "dp.sa.minting"}, nil
}
func (keeper) Delete(context.Context, string, string) error { return nil }

// broker mints a token per store, and refuses the stores in refuse.
type broker struct {
	refuse  map[string]bool
	minted  []credentials.MintRequest
	revoked []string
}

func (b *broker) Mint(_ context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	if b.refuse[r.Store.ID()] {
		return credentials.Minted{}, fmt.Errorf("secret store %s: %w: answered 403", r.Store.ID(), credentials.ErrStoreRefused)
	}
	b.minted = append(b.minted, r)
	return credentials.Minted{
		Credential: credentials.BrokeredCredential{Token: "minted-" + r.Store.ID()},
		ExpiresAt:  clock.Now().Add(credentials.BrokeredLifetime), ProviderID: "slug-" + r.Store.ID(),
	}, nil
}
func (b *broker) Revoke(_ context.Context, r credentials.RevokeRequest) (bool, error) {
	b.revoked = append(b.revoked, r.ProviderID)
	return true, nil
}

func brokered(t *testing.T) (*credentials.SecretStore, *credentials.SecretStore) {
	t.Helper()
	newStore := func() *credentials.SecretStore {
		s, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
			OrganizationID: org, WorkspaceID: "wsp_a", DisplayName: "Doppler", Kind: secretstore.KindDoppler,
			Settings:       credentials.Settings{Doppler: &credentials.DopplerSettings{Project: "payments", Config: "prd"}},
			Access:         credentials.Access{Mode: credentials.AccessBrokered},
			EnvironmentIDs: []string{"env_01BX5ZZKBKACTAV9WEVGEMMVRA"},
		}, nil)
		require.NoError(t, err)
		_, err = s.SetCredential("prn_ada", credentials.MintingCredential{Token: "dp.sa.minting"})
		require.NoError(t, err)
		return s
	}
	return newStore(), newStore()
}

func brokeredSecret(store *credentials.SecretStore, path string) delivery.AssignmentSecret {
	return delivery.AssignmentSecret{Name: path, Store: delivery.StoreSettings{ID: store.ID(), Kind: delivery.StoreDoppler,
		Access: delivery.StoreAccess{Mode: delivery.StoreAccessBrokered}}, Ref: delivery.SecretRef{Path: binding.Escape(path)}}
}

// A brokered store gets a credential minted for the Deployment, with the
// paths it reads there; the grant names it by the store's ID for it.
func TestGrantMintsForBrokeredStores(t *testing.T) {
	w, _, _ := newWorld(t, false)
	one, two := brokered(t)
	w.assignment.Apply.Secrets = []delivery.AssignmentSecret{brokeredSecret(one, "API_KEY"), brokeredSecret(one, "#{literal}"), brokeredSecret(two, "DB")}
	b := &broker{}
	svc, err := NewService(WithAssignments(w), WithApplications(w), WithGrantRepo(w), WithStoreRepo(stores{one.ID(): one, two.ID(): two}),
		WithBroker(b, keeper{}), WithTransactor(w), WithAuditRecorder(w))
	require.NoError(t, err)

	got, err := svc.Grant(context.Background(), agent, "dpl_a")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, credentials.AccessBrokered, got[0].Mode)
	assert.Equal(t, &credentials.BrokeredCredential{Token: "minted-" + one.ID()}, got[0].Credential)
	assert.Equal(t, []string{"API_KEY", "#{literal}"}, b.minted[0].Paths, "the paths as the store holds them")
	assert.Equal(t, "dp.sa.minting", b.minted[0].Credential.Token)
	require.Len(t, w.grants, 2)
	assert.Equal(t, "slug-"+one.ID(), w.grants[0].Snapshot().TokenID)
	recorded, err := json.Marshal(w.audit)
	require.NoError(t, err)
	assert.NotContains(t, string(recorded), "minted-", "never what was granted")
	assert.NotContains(t, string(recorded), "dp.sa.minting")
}

// A store that refuses fails the whole grant, and what this call already
// minted is revoked.
func TestGrantIsAllOrNothing(t *testing.T) {
	w, _, _ := newWorld(t, false)
	one, two := brokered(t)
	w.assignment.Apply.Secrets = []delivery.AssignmentSecret{brokeredSecret(one, "API_KEY"), brokeredSecret(two, "DB")}
	b := &broker{refuse: map[string]bool{two.ID(): true}}
	svc, err := NewService(WithAssignments(w), WithApplications(w), WithGrantRepo(w), WithStoreRepo(stores{one.ID(): one, two.ID(): two}),
		WithBroker(b, keeper{}), WithTransactor(w), WithAuditRecorder(w))
	require.NoError(t, err)

	_, err = svc.Grant(context.Background(), agent, "dpl_a")
	require.ErrorIs(t, err, credentials.ErrStoreRefused)
	assert.Contains(t, err.Error(), two.ID(), "naming the store")
	assert.Equal(t, []string{"slug-" + one.ID()}, b.revoked)
	assert.Empty(t, w.grants)
}

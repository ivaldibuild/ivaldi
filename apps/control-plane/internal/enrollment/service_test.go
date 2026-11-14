package enrollment

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/workloadauth"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	org            = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	clusterID      = "cls_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	otherClusterID = "cls_01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

var (
	t0  = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ada = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: identity.ActorUser}
	ctx = context.Background()
)

type store struct {
	workloads   map[string]*iam.Workload
	tokens      map[string]*iam.WorkloadEnrollmentToken
	byHash      map[string]*iam.WorkloadEnrollmentToken
	credentials map[string]*iam.WorkloadCredential
	clusters    map[string]*delivery.Cluster
	audit       []audit.Event
	envelopes   []events.Envelope
	inTx        bool
	verifiedTx  []bool
}

func newStore(t *testing.T) *store {
	newCluster := func(id, slug string) *delivery.Cluster {
		return must(delivery.NewCluster(delivery.NewClusterProps{
			OrganizationID: org, Slug: slug, DisplayName: slug, Runtime: delivery.RuntimeKubernetes,
		}, ptr(id)))
	}
	return &store{
		workloads: map[string]*iam.Workload{}, tokens: map[string]*iam.WorkloadEnrollmentToken{},
		byHash: map[string]*iam.WorkloadEnrollmentToken{}, credentials: map[string]*iam.WorkloadCredential{},
		clusters: map[string]*delivery.Cluster{
			clusterID:      newCluster(clusterID, "cluster"),
			otherClusterID: newCluster(otherClusterID, "other"),
		},
	}
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	s.inTx = true
	defer func() { s.inTx = false }()
	return fn(ctx)
}

type fakeWorkloads struct{ *store }

func (f fakeWorkloads) Get(_ context.Context, organizationID, principalID string) (*iam.Workload, error) {
	w, ok := f.workloads[principalID]
	if !ok || w.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return w, nil
}
func (f fakeWorkloads) Write(_ context.Context, w *iam.Workload) error {
	f.workloads[w.ID()] = w
	return nil
}
func (f fakeWorkloads) UpdateLastSeen(context.Context, *iam.Workload) error { return nil }
func (f fakeWorkloads) GetPlatform(context.Context, string) (*iam.Workload, error) {
	return nil, iam.ErrNotFound
}
func (f fakeWorkloads) ListPlatform(context.Context) ([]*iam.Workload, error) { return nil, nil }

type fakeTokens struct{ *store }

func (f fakeTokens) Write(_ context.Context, t *iam.WorkloadEnrollmentToken) error {
	f.tokens[t.ID()] = t
	f.byHash[string(t.TokenHash())] = t
	return nil
}
func (f fakeTokens) GetByHashForUpdate(_ context.Context, hash []byte) (*iam.WorkloadEnrollmentToken, error) {
	t, ok := f.byHash[string(hash)]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return t, nil
}

type fakeCredentials struct{ *store }

func (f fakeCredentials) Write(_ context.Context, c *iam.WorkloadCredential) error {
	f.credentials[c.ID()] = c
	return nil
}
func (f fakeCredentials) Get(_ context.Context, organizationID, credentialID string) (*iam.WorkloadCredential, error) {
	c, ok := f.credentials[credentialID]
	if !ok || c.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return c, nil
}
func (f fakeCredentials) GetBySecretHash(_ context.Context, hash []byte) (*iam.WorkloadCredential, error) {
	for _, c := range f.credentials {
		if string(c.SecretHash()) == string(hash) {
			return c, nil
		}
	}
	return nil, iam.ErrNotFound
}
func (f fakeCredentials) GetByKindAndIdentifier(_ context.Context, kind iam.WorkloadCredentialKind, identifier string) (*iam.WorkloadCredential, error) {
	for _, c := range f.credentials {
		if c.Kind() == kind && c.Identifier() == identifier && !c.Revoked() {
			return c, nil
		}
	}
	return nil, iam.ErrNotFound
}
func (f fakeCredentials) ListActiveByIdentity(_ context.Context, organizationID string, kind iam.WorkloadCredentialKind, identifier string) ([]*iam.WorkloadCredential, error) {
	var out []*iam.WorkloadCredential
	for _, c := range f.credentials {
		if c.OrganizationID() == organizationID && c.Kind() == kind && c.Identifier() == identifier && !c.Revoked() {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f fakeCredentials) ListPlatformByPrincipal(context.Context, string) ([]*iam.WorkloadCredential, error) {
	return nil, nil
}

func (f fakeCredentials) ListByPrincipal(_ context.Context, organizationID, principalID string) ([]*iam.WorkloadCredential, error) {
	var out []*iam.WorkloadCredential
	for _, c := range f.credentials {
		if c.OrganizationID() == organizationID && c.PrincipalID() == principalID {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakePrincipals struct{ *store }

func (f fakePrincipals) Get(_ context.Context, organizationID, principalID string) (iam.PrincipalSnapshot, error) {
	if w, ok := f.workloads[principalID]; ok && w.OrganizationID() == organizationID {
		return w.Snapshot().PrincipalSnapshot, nil
	}
	return iam.PrincipalSnapshot{}, iam.ErrNotFound
}

type fakeClusters struct{ *store }

func (f fakeClusters) GetCluster(_ context.Context, organizationID, id string) (*delivery.Cluster, error) {
	c, ok := f.clusters[id]
	if !ok || c.OrganizationID() != organizationID {
		return nil, delivery.ErrNotFound
	}
	return c, nil
}

func (f fakeClusters) GetClusterForUpdate(ctx context.Context, organizationID, id string) (*delivery.Cluster, error) {
	return f.GetCluster(ctx, organizationID, id)
}

func (f fakeClusters) GetClusterByAgentPrincipalID(_ context.Context, organizationID, principalID string) (*delivery.Cluster, error) {
	for _, c := range f.clusters {
		if c.OrganizationID() == organizationID && c.AgentPrincipalID() != nil && *c.AgentPrincipalID() == principalID && !c.Archived() {
			return c, nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (f fakeClusters) UpdateCluster(_ context.Context, c *delivery.Cluster) error {
	f.clusters[c.ID()] = c
	return nil
}

// fakeKubernetes records whether each verification ran inside a transaction.
type fakeKubernetes struct{ *store }

func (f fakeKubernetes) Verify(context.Context, string) (workloadauth.KubernetesIdentity, error) {
	f.verifiedTx = append(f.verifiedTx, f.inTx)
	return workloadauth.KubernetesIdentity{
		Issuer: "https://kubernetes.example", Subject: "system:serviceaccount:default:ivaldi-agent",
		Audience: "ivaldi", ExpiresAt: t0.Add(time.Hour),
	}, nil
}

type allow struct{}

func (allow) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: "dcn_01ARZ3NDEKTSV4RRFFQ69G5FAV", Outcome: authorization.OutcomeAllow}, nil
}

type auditLog struct{ *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.audit = append(a.audit, e)
	return nil
}

type outboxLog struct{ *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.envelopes = append(o.envelopes, envs...)
	return nil
}

type world struct {
	*store
	svc Service
}

func newWorld(t *testing.T, opts ...ServiceOption) *world {
	t.Helper()
	clock.Fix(t, t0)
	s := newStore(t)
	base := []ServiceOption{
		WithWorkloadRepo(fakeWorkloads{s}), WithEnrollmentTokenRepo(fakeTokens{s}),
		WithCredentialRepo(fakeCredentials{s}), WithPrincipalRepo(fakePrincipals{s}),
		WithClusters(fakeClusters{s}),
		WithKubernetesVerifier(fakeKubernetes{s}), WithAuthorizer(allow{}),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
		WithInstall(InstallDetails{
			ServerURL: "https://ivaldi.example.com", Chart: "oci://ghcr.io/ivaldibuild/charts/ivaldi-agent",
			ChartVersion: "1.4.0", CredentialKinds: []string{"bearer", "kubernetes"},
		}),
	}
	svc, err := NewService(append(base, opts...)...)
	require.NoError(t, err)
	return &world{store: s, svc: svc}
}

func TestCreateRefusesAProjectedTokenNoIssuerTrusts(t *testing.T) {
	w := newWorld(t, WithInstall(InstallDetails{
		ServerURL: "https://ivaldi.example.com", Chart: "oci://ghcr.io/ivaldibuild/charts/ivaldi-agent",
		ChartVersion: "1.4.0", CredentialKinds: []string{"bearer"},
	}))
	_, err := w.svc.Create(ctx, ada, CreateInput{OrganizationID: org, ClusterID: clusterID, Method: MethodKubernetes})
	require.ErrorIs(t, err, delivery.ErrInvalid)
	assert.Empty(t, w.tokens, "a token that could never be redeemed is not minted")
}

func TestCreateEnrolment(t *testing.T) {
	w := newWorld(t)
	issued, err := w.svc.Create(ctx, ada, CreateInput{OrganizationID: org, ClusterID: clusterID, Method: MethodBearer})
	require.NoError(t, err)
	assert.Contains(t, issued.Token, iam.WorkloadEnrollmentTokenPrefix)
	assert.Equal(t, clusterID, issued.ClusterID)
	assert.Equal(t, MethodBearer, issued.Method)
	assert.Equal(t, "cluster:"+clusterID, issued.Pool)
	assert.Equal(t, "https://ivaldi.example.com", issued.Install.ServerURL)
	assert.Equal(t, "1.4.0", issued.Install.ChartVersion)
	assert.Equal(t, []string{"bearer", "kubernetes"}, issued.Install.CredentialKinds)
	assert.NotEmpty(t, w.tokens)
	assert.Equal(t, iam.EventWorkloadEnrollmentCreated, w.audit[0].Type)
}

func TestRedeemBearer(t *testing.T) {
	w := newWorld(t)
	issued := mustCreate(t, w, MethodBearer)
	got, err := w.svc.Redeem(ctx, RedeemInput{Token: issued.Token, Method: MethodBearer})
	require.NoError(t, err)
	assert.Contains(t, *got.WorkloadToken, iam.WorkloadTokenPrefix)
	assert.Equal(t, iam.CredentialBearerToken, got.CredentialKind)
	assert.NotNil(t, got.ExpiresAt)
	assert.Equal(t, got.WorkloadPrincipalID, *w.clusters[clusterID].AgentPrincipalID())
	assert.True(t, w.tokens[mustOneID(w.tokens)].Used())
}

func TestRedeemMTLSAndKubernetes(t *testing.T) {
	t.Run("mtls", func(t *testing.T) {
		w := newWorld(t)
		issued := mustCreate(t, w, MethodMTLS)
		ends := t0.Add(2 * time.Hour)
		got, err := w.svc.Redeem(ctx, RedeemInput{
			Token: issued.Token, Method: MethodMTLS, ClientCertificateFP: "fingerprint", ClientCertificateEnds: ends,
		})
		require.NoError(t, err)
		assert.Nil(t, got.WorkloadToken)
		assert.Equal(t, iam.CredentialMTLSCertificate, got.CredentialKind)
		assert.Equal(t, &ends, got.ExpiresAt)
	})
	t.Run("kubernetes", func(t *testing.T) {
		w := newWorld(t)
		issued := mustCreate(t, w, MethodKubernetes)
		got, err := w.svc.Redeem(ctx, RedeemInput{
			Token: issued.Token, Method: MethodKubernetes, KubernetesToken: "projected",
		})
		require.NoError(t, err)
		assert.Nil(t, got.WorkloadToken)
		assert.Equal(t, iam.CredentialKubernetesServiceAccount, got.CredentialKind)
		assert.Equal(t, []bool{false}, w.verifiedTx, "the token is verified before the transaction takes its locks")
	})
}

func TestReenrolmentRevokesOldWorker(t *testing.T) {
	w := newWorld(t)
	first := mustCreate(t, w, MethodBearer)
	old, err := w.svc.Redeem(ctx, RedeemInput{Token: first.Token, Method: MethodBearer})
	require.NoError(t, err)
	oldWorkload := w.workloads[old.WorkloadPrincipalID]
	oldCredential := w.credentials[old.CredentialID]

	second := mustCreate(t, w, MethodBearer)
	next, err := w.svc.Redeem(ctx, RedeemInput{Token: second.Token, Method: MethodBearer})
	require.NoError(t, err)
	assert.Equal(t, iam.PrincipalSuspended, oldWorkload.Status())
	assert.True(t, oldCredential.Revoked())
	assert.Equal(t, next.WorkloadPrincipalID, *w.clusters[clusterID].AgentPrincipalID())
}

func TestRenewBearer(t *testing.T) {
	w := newWorld(t)
	got := mustRedeem(t, w, MethodBearer)
	caller := identity.Caller{
		Actor:          identity.Actor{PrincipalID: got.WorkloadPrincipalID, Kind: identity.ActorWorkload},
		OrganizationID: org, CredentialID: got.CredentialID, CredentialExpiresAt: *got.ExpiresAt,
	}
	renewed, err := w.svc.Renew(ctx, caller)
	require.NoError(t, err)
	assert.Contains(t, *renewed.WorkloadToken, iam.WorkloadTokenPrefix)
	assert.NotEqual(t, got.CredentialID, renewed.CredentialID)
	old := w.credentials[got.CredentialID]
	assert.Equal(t, renewed.CredentialID, *old.ReplacedBy())
	assert.Equal(t, t0.Add(5*time.Minute), *old.NotAfter())
}

func TestRenewAuditsTheCredentialWithoutItsToken(t *testing.T) {
	w := newWorld(t)
	got := mustRedeem(t, w, MethodBearer)
	renewed, err := w.svc.Renew(ctx, bearerCaller(got))
	require.NoError(t, err)

	record := w.audit[len(w.audit)-1]
	require.Equal(t, iam.EventWorkloadCredentialRenewed, record.Type)
	assert.Equal(t, renewed.CredentialID, record.ResourceID)
	body, err := json.Marshal(record.After)
	require.NoError(t, err)
	assert.NotContains(t, string(body), *renewed.WorkloadToken)
}

func TestRenewRefusesASupersededCredential(t *testing.T) {
	w := newWorld(t)
	got := mustRedeem(t, w, MethodBearer)
	_, err := w.svc.Renew(ctx, bearerCaller(got))
	require.NoError(t, err)
	credentials := len(w.credentials)

	// Within the grace period the old token still authenticates, but it must
	// not mint a second successor.
	_, err = w.svc.Renew(ctx, bearerCaller(got))
	assert.ErrorIs(t, err, iam.ErrStatus)
	assert.Len(t, w.credentials, credentials)
}

func bearerCaller(got *Redeemed) identity.Caller {
	return identity.Caller{
		Actor:          identity.Actor{PrincipalID: got.WorkloadPrincipalID, Kind: identity.ActorWorkload},
		OrganizationID: org, CredentialID: got.CredentialID, CredentialExpiresAt: *got.ExpiresAt,
	}
}

func TestRenewMTLSAndKubernetes(t *testing.T) {
	t.Run("mtls", func(t *testing.T) {
		w := newWorld(t)
		issued := mustCreate(t, w, MethodMTLS)
		// The certificate outlives the credential record, so renewal extends
		// the record by a TTL without passing the certificate.
		certEnds := t0.Add(72 * time.Hour)
		got, err := w.svc.Redeem(ctx, RedeemInput{
			Token: issued.Token, Method: MethodMTLS, ClientCertificateFP: "fingerprint", ClientCertificateEnds: certEnds,
		})
		require.NoError(t, err)
		require.Equal(t, t0.Add(24*time.Hour), *got.ExpiresAt)

		clock.Fix(t, t0.Add(12*time.Hour))
		caller := identity.Caller{
			Actor:          identity.Actor{PrincipalID: got.WorkloadPrincipalID, Kind: identity.ActorWorkload},
			OrganizationID: org, CredentialID: got.CredentialID, CredentialExpiresAt: certEnds,
		}
		renewed, err := w.svc.Renew(ctx, caller)
		require.NoError(t, err)
		assert.Equal(t, got.CredentialID, renewed.CredentialID)
		assert.Nil(t, renewed.WorkloadToken)
		assert.Equal(t, t0.Add(36*time.Hour), *renewed.ExpiresAt)
		assert.Equal(t, t0.Add(24*time.Hour), *renewed.PreviousExpiresAt)
	})
	t.Run("kubernetes", func(t *testing.T) {
		w := newWorld(t)
		got := mustRedeem(t, w, MethodKubernetes)
		caller := identity.Caller{
			Actor:          identity.Actor{PrincipalID: got.WorkloadPrincipalID, Kind: identity.ActorWorkload},
			OrganizationID: org, CredentialID: got.CredentialID, CredentialExpiresAt: t0.Add(time.Hour),
		}
		renewed, err := w.svc.Renew(ctx, caller)
		require.NoError(t, err)
		assert.Equal(t, got.CredentialID, renewed.CredentialID)
		assert.Equal(t, t0.Add(24*time.Hour), *renewed.ExpiresAt)
	})
}

func TestRenewRefusedWhenNoLongerClusterAgent(t *testing.T) {
	w := newWorld(t)
	got := mustRedeem(t, w, MethodBearer)
	cluster := w.clusters[clusterID]
	require.NoError(t, cluster.UnassignAgent())
	w.clusters[clusterID] = cluster
	caller := identity.Caller{
		Actor:          identity.Actor{PrincipalID: got.WorkloadPrincipalID, Kind: identity.ActorWorkload},
		OrganizationID: org, CredentialID: got.CredentialID, CredentialExpiresAt: *got.ExpiresAt,
	}
	_, err := w.svc.Renew(ctx, caller)
	assert.ErrorIs(t, err, delivery.ErrNotClusterAgent)
}

func mustCreate(t *testing.T, w *world, method CredentialMethod) *IssuedEnrolment {
	t.Helper()
	issued, err := w.svc.Create(ctx, ada, CreateInput{OrganizationID: org, ClusterID: clusterID, Method: method})
	require.NoError(t, err)
	return issued
}

func mustRedeem(t *testing.T, w *world, method CredentialMethod) *Redeemed {
	t.Helper()
	issued := mustCreate(t, w, method)
	in := RedeemInput{Token: issued.Token, Method: method}
	switch method {
	case MethodMTLS:
		in.ClientCertificateFP = "fingerprint"
		in.ClientCertificateEnds = t0.Add(2 * time.Hour)
	case MethodKubernetes:
		in.KubernetesToken = "projected"
	}
	got, err := w.svc.Redeem(ctx, in)
	require.NoError(t, err)
	return got
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func mustOneID[V any](m map[string]V) string {
	for id := range m {
		return id
	}
	panic("empty map")
}

func TestReenrolmentReusesCertificateIdentity(t *testing.T) {
	w := newWorld(t)
	first := mustRedeem(t, w, MethodMTLS)
	firstCredential := w.credentials[first.CredentialID]

	second := mustRedeem(t, w, MethodMTLS)
	assert.NotEqual(t, first.CredentialID, second.CredentialID)
	assert.True(t, firstCredential.Revoked(), "the previous certificate credential is revoked")
	assert.Equal(t, second.WorkloadPrincipalID, *w.clusters[clusterID].AgentPrincipalID())
}

func TestReenrolmentReusesKubernetesIdentity(t *testing.T) {
	w := newWorld(t)
	first := mustRedeem(t, w, MethodKubernetes)
	firstCredential := w.credentials[first.CredentialID]

	second := mustRedeem(t, w, MethodKubernetes)
	assert.NotEqual(t, first.CredentialID, second.CredentialID)
	assert.True(t, firstCredential.Revoked(), "the previous service-account credential is revoked")
}

func TestAgentStatus(t *testing.T) {
	w := newWorld(t)
	got := mustRedeem(t, w, MethodBearer)

	status, err := w.svc.Status(ctx, ada, org, clusterID)
	require.NoError(t, err)
	assert.Equal(t, clusterID, status.ClusterID)
	require.NotNil(t, status.AgentPrincipalID)
	assert.Equal(t, got.WorkloadPrincipalID, *status.AgentPrincipalID)
	assert.Equal(t, "active", status.Status)
	assert.NotNil(t, status.RedeemedAt)
	assert.Nil(t, status.LastFailure)
}

func TestReenrolmentRefusesAnIdentityUsedByAnotherCluster(t *testing.T) {
	w := newWorld(t)
	first := mustRedeem(t, w, MethodMTLS)

	issued, err := w.svc.Create(ctx, ada, CreateInput{OrganizationID: org, ClusterID: otherClusterID, Method: MethodMTLS})
	require.NoError(t, err)
	_, err = w.svc.Redeem(ctx, RedeemInput{
		Token: issued.Token, Method: MethodMTLS,
		ClientCertificateFP: "fingerprint", ClientCertificateEnds: t0.Add(2 * time.Hour),
	})
	require.ErrorIs(t, err, delivery.ErrIdentityInUse)

	assert.Equal(t, first.WorkloadPrincipalID, *w.clusters[clusterID].AgentPrincipalID(), "the first cluster keeps its agent")
	assert.Nil(t, w.clusters[otherClusterID].AgentPrincipalID())
	require.NotNil(t, w.clusters[otherClusterID].AgentLastFailure())
	assert.Contains(t, w.clusters[otherClusterID].AgentLastFailure().Reason, "identity_in_use")
	assert.Contains(t, w.clusters[otherClusterID].AgentLastFailure().Reason, "cluster")

	refused := w.audit[len(w.audit)-1]
	assert.Equal(t, iam.EventWorkloadEnrollmentRefused, refused.Type, "the refusal is audited")
	assert.True(t, refused.Actor.IsSystem(), "the caller holds only a token, so Ivaldi is the actor")
	assert.Equal(t, refusedEnrolment{ClusterID: otherClusterID, Failure: w.clusters[otherClusterID].AgentLastFailure()}, refused.After)
}

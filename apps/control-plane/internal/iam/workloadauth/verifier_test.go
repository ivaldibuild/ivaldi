package workloadauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type fakeCredentials struct {
	byHash map[string]*iam.WorkloadCredential
	byID   map[string]*iam.WorkloadCredential
}

func (f fakeCredentials) GetBySecretHash(_ context.Context, hash []byte) (*iam.WorkloadCredential, error) {
	c, ok := f.byHash[string(hash)]
	if !ok {
		return nil, iam.ErrNotFound
	}
	return c, nil
}

func (f fakeCredentials) GetByKindAndIdentifier(_ context.Context, kind iam.WorkloadCredentialKind, identifier string) (*iam.WorkloadCredential, error) {
	for _, c := range f.byID {
		if c.Kind() == kind && c.Identifier() == identifier {
			return c, nil
		}
	}
	return nil, iam.ErrNotFound
}

type fakeWorkloads struct{ w *iam.Workload }

func (f fakeWorkloads) Get(_ context.Context, organizationID, principalID string) (*iam.Workload, error) {
	if f.w.ID() != principalID || f.w.OrganizationID() != organizationID {
		return nil, iam.ErrNotFound
	}
	return f.w, nil
}
func (f fakeWorkloads) UpdateLastSeen(context.Context, *iam.Workload) error { return nil }

type fakeNext struct{ called bool }

func (f *fakeNext) Verify(context.Context, string) (*identity.Claims, error) {
	f.called = true
	return nil, identity.ErrInvalidToken
}

type fakeKubernetes struct{}

func (fakeKubernetes) Verify(context.Context, string) (KubernetesIdentity, error) {
	return KubernetesIdentity{
		Issuer: "https://kubernetes.example", Subject: "system:serviceaccount:default:agent",
		Audience: "ivaldi", ExpiresAt: time.Date(2026, 11, 7, 13, 0, 0, 0, time.UTC),
	}, nil
}

func testWorkload(t *testing.T) *iam.Workload {
	t.Helper()
	w, err := iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV", DisplayName: "Agent"},
		Component:      iam.ComponentRuntimeAgent, Pool: "target:tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV",
	}, nil)
	require.NoError(t, err)
	return w
}

func TestVerifierKinds(t *testing.T) {
	clock.Fix(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	w := testWorkload(t)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)

	bearer, err := iam.NewWorkloadTokenSecret()
	require.NoError(t, err)
	bearerCred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: w.OrganizationID(), PrincipalID: w.ID(), Kind: iam.CredentialBearerToken,
		Identifier: bearer.Prefix, SecretHash: bearer.Hash, NotAfter: &expires,
	}, nil)
	require.NoError(t, err)
	mtlsCred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: w.OrganizationID(), PrincipalID: w.ID(), Kind: iam.CredentialMTLSCertificate,
		Identifier: "fingerprint", NotAfter: &expires,
	}, nil)
	require.NoError(t, err)
	kubeCred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: w.OrganizationID(), PrincipalID: w.ID(), Kind: iam.CredentialKubernetesServiceAccount,
		Identifier: KubernetesIdentifier(KubernetesIdentity{
			Issuer: "https://kubernetes.example", Subject: "system:serviceaccount:default:agent", Audience: "ivaldi",
		}), NotAfter: &expires,
	}, nil)
	require.NoError(t, err)
	creds := fakeCredentials{
		byHash: map[string]*iam.WorkloadCredential{string(bearer.Hash): bearerCred},
		byID: map[string]*iam.WorkloadCredential{
			bearerCred.ID(): bearerCred, mtlsCred.ID(): mtlsCred, kubeCred.ID(): kubeCred,
		},
	}
	next := &fakeNext{}
	v, err := NewVerifier(creds, fakeWorkloads{w}, fakeKubernetes{}, next)
	require.NoError(t, err)

	claims, err := v.Verify(context.Background(), bearer.Secret)
	require.NoError(t, err)
	assert.Equal(t, identity.ActorWorkload, claims.Kind)
	assert.Equal(t, bearerCred.ID(), claims.ID)

	// A certificate outlives the record, so the claims carry the
	// certificate's expiry for renewal to extend the record.
	certExpiry := now.Add(2 * time.Hour)
	claims, err = v.Verify(context.Background(), identity.MTLSCredential("fingerprint", certExpiry))
	require.NoError(t, err)
	assert.Equal(t, mtlsCred.ID(), claims.ID)
	assert.Equal(t, certExpiry, claims.ExpiresAt)

	claims, err = v.Verify(context.Background(), identity.KubernetesCredential("projected"))
	require.NoError(t, err)
	assert.Equal(t, kubeCred.ID(), claims.ID)

	_, err = v.Verify(context.Background(), "not-a-workload-token")
	assert.ErrorIs(t, err, identity.ErrInvalidToken)
	assert.True(t, next.called, "unknown credentials pass to the next verifier")
}

func TestVerifierRefusesExpiredCredential(t *testing.T) {
	clock.Fix(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	w := testWorkload(t)
	past := time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC)
	bearer, err := iam.NewWorkloadTokenSecret()
	require.NoError(t, err)
	created := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	cred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: w.OrganizationID(), PrincipalID: w.ID(), Kind: iam.CredentialBearerToken,
		Identifier: bearer.Prefix, SecretHash: bearer.Hash, NotAfter: &past, CreatedAt: &created,
	}, nil)
	require.NoError(t, err)
	v, err := NewVerifier(
		fakeCredentials{byHash: map[string]*iam.WorkloadCredential{string(bearer.Hash): cred}, byID: map[string]*iam.WorkloadCredential{}},
		fakeWorkloads{w}, nil, &fakeNext{})
	require.NoError(t, err)
	_, err = v.Verify(context.Background(), bearer.Secret)
	assert.ErrorIs(t, err, identity.ErrTokenExpired)
}

package workloadauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type platformKeyFixture struct {
	verifier   identity.Verifier
	signer     identity.WorkloadKeySigner
	workload   *iam.Workload
	credential *iam.WorkloadCredential
}

func newPlatformKeyFixture(t *testing.T) *platformKeyFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	signer, err := identity.NewWorkloadKeySigner(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	require.NoError(t, err)
	publicDER, err := identity.MarshalWorkloadPublicKeyDER(public)
	require.NoError(t, err)

	workload, err := iam.NewPlatformWorkload("gateway", iam.ComponentGateway)
	require.NoError(t, err)
	credential, err := iam.NewPlatformKeyCredential(workload.ID(), publicDER)
	require.NoError(t, err)
	v, err := NewVerifier(
		fakeCredentials{byID: map[string]*iam.WorkloadCredential{credential.ID(): credential}},
		fakeWorkloads{w: workload}, nil, &fakeNext{})
	require.NoError(t, err)
	return &platformKeyFixture{verifier: v, signer: signer, workload: workload, credential: credential}
}

func (f *platformKeyFixture) verify(t *testing.T, audience string) (*identity.Claims, error) {
	t.Helper()
	assertion, err := f.signer.Sign(audience)
	require.NoError(t, err)
	return f.verifier.Verify(context.Background(), identity.WorkloadKeyCredential(assertion))
}

func TestPlatformWorkloadAuthenticatesWithItsKey(t *testing.T) {
	f := newPlatformKeyFixture(t)
	claims, err := f.verify(t, identity.AudienceControlPlane)
	require.NoError(t, err)
	assert.Equal(t, f.workload.ID(), claims.Subject)
	assert.Empty(t, claims.OrganizationID, "a platform workload acts in no Organization")
	assert.Equal(t, identity.ActorWorkload, claims.Kind)
	assert.Equal(t, ClientKey, claims.ClientID)
	assert.Equal(t, f.credential.ID(), claims.ID)
}

func TestPlatformKeyIsRefused(t *testing.T) {
	t.Run("for another audience", func(t *testing.T) {
		f := newPlatformKeyFixture(t)
		_, err := f.verify(t, "urn:ivaldi:indexer")
		assert.ErrorIs(t, err, identity.ErrInvalidToken)
	})
	t.Run("once revoked", func(t *testing.T) {
		f := newPlatformKeyFixture(t)
		f.credential.Revoke()
		_, err := f.verify(t, identity.AudienceControlPlane)
		assert.ErrorIs(t, err, identity.ErrInvalidToken)
	})
	t.Run("when its workload is suspended", func(t *testing.T) {
		f := newPlatformKeyFixture(t)
		require.NoError(t, f.workload.Suspend())
		_, err := f.verify(t, identity.AudienceControlPlane)
		assert.ErrorIs(t, err, identity.ErrInvalidToken)
	})
	t.Run("when the key is unknown", func(t *testing.T) {
		f := newPlatformKeyFixture(t)
		other := newPlatformKeyFixture(t)
		assertion, err := other.signer.Sign(identity.AudienceControlPlane)
		require.NoError(t, err)
		_, err = f.verifier.Verify(context.Background(), identity.WorkloadKeyCredential(assertion))
		assert.ErrorIs(t, err, identity.ErrInvalidToken)
	})
}

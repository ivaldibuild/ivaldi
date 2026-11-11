package iam

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func testPublicKeyDER(t *testing.T) []byte {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := identity.MarshalWorkloadPublicKeyDER(public)
	require.NoError(t, err)
	return der
}

func TestNewPlatformWorkload(t *testing.T) {
	w, err := NewPlatformWorkload("gateway", ComponentGateway)
	require.NoError(t, err)
	assert.True(t, w.Platform())
	assert.Empty(t, w.OrganizationID())
	assert.Equal(t, PlatformPool, w.Pool())
	assert.True(t, w.Snapshot().Platform)
}

func TestPlatformPrincipalRules(t *testing.T) {
	t.Run("only a platform component", func(t *testing.T) {
		_, err := NewPlatformWorkload("runtime agent", ComponentRuntimeAgent)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("only a workload", func(t *testing.T) {
		_, err := newPrincipal(PrincipalUser, PrincipalProps{Platform: true, DisplayName: "Ada"}, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("no organization", func(t *testing.T) {
		_, err := NewWorkload(NewWorkloadProps{
			PrincipalProps: PrincipalProps{Platform: true, OrganizationID: testOrg, DisplayName: "gateway"},
			Component:      ComponentGateway, Pool: PlatformPool,
		}, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("an Organization workload still needs its organization", func(t *testing.T) {
		_, err := NewWorkload(NewWorkloadProps{
			PrincipalProps: PrincipalProps{DisplayName: "gateway"},
			Component:      ComponentGateway, Pool: "default",
		}, nil)
		assert.ErrorIs(t, err, ErrInvalid, "a missing organization is not read as the platform")
	})
}

func TestNewPlatformKeyCredential(t *testing.T) {
	w, err := NewPlatformWorkload("gateway", ComponentGateway)
	require.NoError(t, err)
	der := testPublicKeyDER(t)

	c, err := NewPlatformKeyCredential(w.ID(), der)
	require.NoError(t, err)
	public, err := identity.ParseWorkloadPublicKeyDER(der)
	require.NoError(t, err)
	thumbprint, err := identity.WorkloadKeyThumbprint(public)
	require.NoError(t, err)
	assert.Equal(t, CredentialPublicKey, c.Kind())
	assert.Equal(t, thumbprint, c.Identifier())
	assert.Equal(t, der, c.PublicKey())
	assert.True(t, c.Platform())
	assert.Empty(t, c.OrganizationID())
}

func TestPublicKeyCredentialRules(t *testing.T) {
	der := testPublicKeyDER(t)
	public, err := identity.ParseWorkloadPublicKeyDER(der)
	require.NoError(t, err)
	thumbprint, err := identity.WorkloadKeyThumbprint(public)
	require.NoError(t, err)
	base := NewWorkloadCredentialProps{
		Platform: true, PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Kind: CredentialPublicKey, Identifier: thumbprint, PublicKey: der,
	}

	t.Run("an Organization workload cannot hold one yet", func(t *testing.T) {
		p := base
		p.Platform, p.OrganizationID = false, testOrg
		_, err := NewWorkloadCredential(p, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("the identifier must be the key's thumbprint", func(t *testing.T) {
		p := base
		p.Identifier = "another-key"
		_, err := NewWorkloadCredential(p, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("the key is required", func(t *testing.T) {
		p := base
		p.PublicKey = nil
		_, err := NewWorkloadCredential(p, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
	t.Run("only a public key credential has a key", func(t *testing.T) {
		p := base
		p.Kind = CredentialMTLSCertificate
		_, err := NewWorkloadCredential(p, nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
}

func TestPlatformComponentAllows(t *testing.T) {
	assert.True(t, PlatformComponentAllows(ComponentGateway, "version_control.webhook_route.resolve"))
	assert.False(t, PlatformComponentAllows(ComponentGateway, "source.read"))
	assert.False(t, PlatformComponentAllows(ComponentWorker, "version_control.webhook_route.resolve"))
}

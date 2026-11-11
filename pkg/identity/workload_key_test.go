package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ed25519PEM(t *testing.T) ([]byte, crypto.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), public
}

func p256PEM(t *testing.T) ([]byte, crypto.PublicKey) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(private)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), private.Public()
}

func TestWorkloadKeyAssertionRoundTrip(t *testing.T) {
	for name, gen := range map[string]func(*testing.T) ([]byte, crypto.PublicKey){
		"ed25519": ed25519PEM,
		"p256":    p256PEM,
	} {
		t.Run(name, func(t *testing.T) {
			privatePEM, public := gen(t)
			signer, err := NewWorkloadKeySigner(privatePEM)
			require.NoError(t, err)
			thumbprint, err := WorkloadKeyThumbprint(public)
			require.NoError(t, err)
			assert.Equal(t, thumbprint, signer.KeyID())

			token, err := signer.Sign(AudienceControlPlane)
			require.NoError(t, err)
			kid, err := WorkloadKeyID(token)
			require.NoError(t, err)
			assert.Equal(t, thumbprint, kid)

			got, err := VerifyWorkloadKeyAssertion(token, public, AudienceControlPlane, time.Now())
			require.NoError(t, err)
			assert.Equal(t, thumbprint, got.KeyID)
			assert.WithinDuration(t, time.Now().Add(workloadAssertionLifetime), got.ExpiresAt, 5*time.Second)
		})
	}
}

func TestVerifyWorkloadKeyAssertionRefuses(t *testing.T) {
	privatePEM, public := ed25519PEM(t)
	signer, err := NewWorkloadKeySigner(privatePEM)
	require.NoError(t, err)
	token, err := signer.Sign(AudienceControlPlane)
	require.NoError(t, err)

	t.Run("another audience", func(t *testing.T) {
		_, err := VerifyWorkloadKeyAssertion(token, public, "urn:ivaldi:indexer", time.Now())
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("an expired assertion", func(t *testing.T) {
		_, err := VerifyWorkloadKeyAssertion(token, public, AudienceControlPlane, time.Now().Add(10*time.Minute))
		assert.ErrorIs(t, err, ErrTokenExpired)
	})
	t.Run("another key", func(t *testing.T) {
		_, other := ed25519PEM(t)
		_, err := VerifyWorkloadKeyAssertion(token, other, AudienceControlPlane, time.Now())
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("a lifetime over the limit", func(t *testing.T) {
		long := signRaw(t, privatePEM, signer.KeyID(), time.Now(), time.Hour)
		_, err := VerifyWorkloadKeyAssertion(long, public, AudienceControlPlane, time.Now())
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("an assertion issued in the future", func(t *testing.T) {
		future := signRaw(t, privatePEM, signer.KeyID(), time.Now().Add(time.Hour), time.Minute)
		_, err := VerifyWorkloadKeyAssertion(future, public, AudienceControlPlane, time.Now())
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("a key ID that is not the key's", func(t *testing.T) {
		borrowed := signRaw(t, privatePEM, "someone-else", time.Now(), time.Minute)
		_, err := VerifyWorkloadKeyAssertion(borrowed, public, AudienceControlPlane, time.Now())
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
}

// signRaw signs an assertion with chosen claims, which the signer would not
// produce.
func signRaw(t *testing.T, privatePEM []byte, kid string, issued time.Time, lifetime time.Duration) string {
	t.Helper()
	key, err := parsePrivateKey(privatePEM)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid))
	require.NoError(t, err)
	token, err := jwt.Signed(signer).Claims(jwt.Claims{
		Audience: jwt.Audience{AudienceControlPlane},
		IssuedAt: jwt.NewNumericDate(issued),
		Expiry:   jwt.NewNumericDate(issued.Add(lifetime)),
	}).Serialize()
	require.NoError(t, err)
	return token
}

func TestWorkloadPublicKeyEncodings(t *testing.T) {
	_, public := ed25519PEM(t)
	der, err := MarshalWorkloadPublicKeyDER(public)
	require.NoError(t, err)

	fromDER, err := ParseWorkloadPublicKeyDER(der)
	require.NoError(t, err)
	fromPEM, err := ParseWorkloadPublicKeyPEM(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	require.NoError(t, err)
	assert.Equal(t, public, fromDER)
	assert.Equal(t, public, fromPEM)
}

func TestWorkloadKeysMustBeEd25519OrP256(t *testing.T) {
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	_, err = WorkloadKeyThumbprint(p384.Public())
	assert.Error(t, err)

	der, err := x509.MarshalECPrivateKey(p384)
	require.NoError(t, err)
	_, err = NewWorkloadKeySigner(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	assert.Error(t, err)
}

func TestWorkloadKeyCredentialIsReserved(t *testing.T) {
	assert.True(t, HasInternalCredentialPrefix(WorkloadKeyCredential("token")),
		"a client cannot send an already-encoded workload-key credential")
}

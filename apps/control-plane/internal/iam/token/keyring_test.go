package token

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const testIssuer = "https://ivaldi.example.com"

func writePEM(t *testing.T, blockType string, der []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600))
	return path
}

func writeRaw(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func pkcs8(t *testing.T, key crypto.Signer) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return writePEM(t, "PRIVATE KEY", der)
}

func ecKey(t *testing.T, curve elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	return k
}

func TestLoadKeyRingReadsEveryFormat(t *testing.T) {
	ec := ecKey(t, elliptic.P256())
	sec1, err := x509.MarshalECPrivateKey(ec)
	require.NoError(t, err)
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rs, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tests := []struct {
		name string
		file string
		alg  string
	}{
		{"PKCS #8 P-256", pkcs8(t, ecKey(t, elliptic.P256())), identity.AlgorithmES256},
		{"SEC 1 P-256", writePEM(t, "EC PRIVATE KEY", sec1), identity.AlgorithmES256},
		{"PKCS #8 Ed25519", pkcs8(t, ed), identity.AlgorithmEdDSA},
		{"PKCS #1 RSA", writePEM(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(rs)), identity.AlgorithmRS256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kr, err := LoadKeyRing([]string{tt.file})
			require.NoError(t, err)
			keys := kr.PublicKeys()
			require.Len(t, keys, 1)
			assert.Equal(t, tt.alg, keys[0].Algorithm)
			assert.Len(t, keys[0].KeyID, 43, "the kid is the base64url SHA-256 thumbprint")
		})
	}
}

func TestLoadKeyRingRefuses(t *testing.T) {
	weakRSA, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	p256 := pkcs8(t, ecKey(t, elliptic.P256()))

	tests := map[string][]string{
		"no files":           nil,
		"missing file":       {filepath.Join(t.TempDir(), "absent.pem")},
		"not PEM":            {writeRaw(t, "hello")},
		"a certificate":      {writePEM(t, "CERTIFICATE", []byte{1, 2, 3})},
		"P-384":              {pkcs8(t, ecKey(t, elliptic.P384()))},
		"short RSA":          {writePEM(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(weakRSA))},
		"the same key twice": {p256, p256},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := LoadKeyRing(files)
			assert.Error(t, err)
		})
	}
}

func TestJWKSPublishesOnlyPublicKeys(t *testing.T) {
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	kr, err := NewKeyRing(ecKey(t, elliptic.P256()), ed)
	require.NoError(t, err)

	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(kr.JWKS(), &set))
	require.Len(t, set.Keys, 2)
	for i, k := range set.Keys {
		assert.Equal(t, kr.PublicKeys()[i].KeyID, k["kid"])
		assert.Equal(t, "sig", k["use"])
		assert.NotContains(t, k, "d", "no private key material")
	}
	assert.Equal(t, "ES256", set.Keys[0]["alg"])
	assert.Equal(t, "EdDSA", set.Keys[1]["alg"])
}

func TestDeriveKeys(t *testing.T) {
	kr, err := NewKeyRing(ecKey(t, elliptic.P256()), ecKey(t, elliptic.P256()))
	require.NoError(t, err)

	cookies, err := kr.DeriveKeys("ivaldi/signin-cookie/v1")
	require.NoError(t, err)
	again, err := kr.DeriveKeys("ivaldi/signin-cookie/v1")
	require.NoError(t, err)
	other, err := kr.DeriveKeys("ivaldi/other/v1")
	require.NoError(t, err)

	require.Len(t, cookies, 2, "one per key, so cookies sealed with a retiring key still open")
	assert.Equal(t, cookies, again, "derivation is stable across calls and restarts")
	assert.NotEqual(t, cookies[0], cookies[1])
	assert.NotEqual(t, cookies[0], other[0], "each purpose gets its own key")
}

func TestIssuedTokensVerify(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	old, err := NewKeyRing(ecKey(t, elliptic.P256()))
	require.NoError(t, err)
	issuer, err := NewIssuer(old, testIssuer, 15*time.Minute)
	require.NoError(t, err)

	tok, err := issuer.Issue(context.Background(), Subject{
		OrganizationID: "org_1", PrincipalID: "prn_1", Kind: identity.ActorUser,
		ClientID: "ivaldictl", Resource: testIssuer, Scopes: []string{"read"},
	})
	require.NoError(t, err)
	assert.Equal(t, now.Add(15*time.Minute), tok.ExpiresAt)

	// After rotation the new key signs, and the old one still verifies.
	rotated, err := NewKeyRing(ecKey(t, elliptic.P256()), old.(*keyRing).keys[0].private)
	require.NoError(t, err)
	v, err := identity.NewVerifier(identity.VerifierConfig{
		Issuer: testIssuer, Audience: testIssuer,
		Keys: identity.NewStaticKeySource(rotated.PublicKeys()...),
		Now:  func() time.Time { return now },
	})
	require.NoError(t, err)

	claims, err := v.Verify(context.Background(), tok.Value)
	require.NoError(t, err)
	assert.Equal(t, "prn_1", claims.Subject)
	assert.Equal(t, "org_1", claims.OrganizationID)
	assert.Equal(t, tok.ID, claims.ID)
	assert.Equal(t, []string{"read"}, claims.Scopes)
	assert.Equal(t, identity.ActorUser, claims.Kind)
}

func TestNewIssuerNeedsItsConfig(t *testing.T) {
	kr, err := NewEphemeralKeyRing()
	require.NoError(t, err)

	_, err = NewIssuer(nil, testIssuer, time.Minute)
	assert.Error(t, err)
	_, err = NewIssuer(kr, "", time.Minute)
	assert.Error(t, err)
	_, err = NewIssuer(kr, testIssuer, 0)
	assert.Error(t, err)
}

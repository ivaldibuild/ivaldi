// Package token signs the control plane's access tokens and publishes the
// keys that verify them. One platform issuer, the control plane's external
// URL, signs with keys from its configuration (ADR 0012); pkg/identity
// verifies the tokens offline.
package token

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/go-jose/go-jose/v4"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// KeyRing holds the signing keys: the first signs, and all verify, so a key
// can be rotated out once the tokens it signed have expired.
type KeyRing interface {
	// Sign returns an access token carrying the claims, signed with the
	// active key.
	Sign(c identity.Claims) (string, error)
	// SignJWT returns a JWT of the type typ (its "typ" header) carrying
	// claims, which encode as a JSON object, signed with the active key.
	SignJWT(typ string, claims any) (string, error)
	// PublicKeys returns every key's public half, for verifying tokens.
	PublicKeys() []identity.PublicKey
	// JWKS returns the public keys as a JSON Web Key Set (RFC 7517), for
	// publishing.
	JWKS() []byte
	// DeriveKeys returns one 32-byte secret per key, active key first, for
	// purpose, such as encrypting cookies. Each purpose gets its own keys,
	// and they rotate with the signing keys.
	DeriveKeys(purpose string) ([][32]byte, error)
}

const minRSABits = 2048

type signingKey struct {
	kid     string
	alg     jose.SignatureAlgorithm
	private crypto.Signer
	public  jose.JSONWebKey
}

type keyRing struct {
	keys []signingKey
	jwks []byte
}

// LoadKeyRing reads PEM private keys from files: PKCS #8, SEC 1 EC, or
// PKCS #1 RSA. Each key's type decides its algorithm: a P-256 key signs
// ES256, an Ed25519 key EdDSA, and an RSA key of at least 2048 bits RS256.
func LoadKeyRing(files []string) (KeyRing, error) {
	if len(files) == 0 {
		return nil, errors.New("token: no signing key files")
	}
	signers := make([]crypto.Signer, 0, len(files))
	for _, f := range files {
		s, err := readPrivateKey(f)
		if err != nil {
			return nil, err
		}
		signers = append(signers, s)
	}
	return NewKeyRing(signers...)
}

// NewEphemeralKeyRing returns a KeyRing with a P-256 key generated now. Its
// tokens stop verifying when the process exits and no other process can
// verify them, so it is for local development and tests only.
func NewEphemeralKeyRing() (KeyRing, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("token: generate key: %w", err)
	}
	return NewKeyRing(k)
}

// NewKeyRing returns a KeyRing over the keys; the first signs.
func NewKeyRing(signers ...crypto.Signer) (KeyRing, error) {
	if len(signers) == 0 {
		return nil, errors.New("token: no signing keys")
	}
	r := &keyRing{}
	set := jose.JSONWebKeySet{}
	seen := map[string]bool{}
	for _, s := range signers {
		alg, err := algorithmOf(s)
		if err != nil {
			return nil, err
		}
		public := jose.JSONWebKey{Key: s.Public(), Algorithm: string(alg), Use: "sig"}
		thumb, err := public.Thumbprint(crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("token: key thumbprint: %w", err)
		}
		public.KeyID = base64.RawURLEncoding.EncodeToString(thumb)
		if seen[public.KeyID] {
			return nil, fmt.Errorf("token: key %s is configured twice", public.KeyID)
		}
		seen[public.KeyID] = true
		r.keys = append(r.keys, signingKey{kid: public.KeyID, alg: alg, private: s, public: public})
		set.Keys = append(set.Keys, public)
	}
	jwks, err := json.Marshal(set)
	if err != nil {
		return nil, fmt.Errorf("token: encode JWKS: %w", err)
	}
	r.jwks = jwks
	return r, nil
}

func (r *keyRing) Sign(c identity.Claims) (string, error) {
	return r.SignJWT(identity.AccessTokenType, c)
}

func (r *keyRing) SignJWT(typ string, claims any) (string, error) {
	active := r.keys[0]
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: active.alg, Key: jose.JSONWebKey{Key: active.private, KeyID: active.kid}},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)),
	)
	if err != nil {
		return "", fmt.Errorf("token: signer: %w", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("token: encode claims: %w", err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("token: sign: %w", err)
	}
	return jws.CompactSerialize()
}

func (r *keyRing) PublicKeys() []identity.PublicKey {
	keys := make([]identity.PublicKey, len(r.keys))
	for i, k := range r.keys {
		keys[i] = identity.PublicKey{KeyID: k.kid, Algorithm: string(k.alg), Key: k.private.Public()}
	}
	return keys
}

func (r *keyRing) JWKS() []byte { return append([]byte(nil), r.jwks...) }

func (r *keyRing) DeriveKeys(purpose string) ([][32]byte, error) {
	out := make([][32]byte, len(r.keys))
	for i, k := range r.keys {
		secret, err := x509.MarshalPKCS8PrivateKey(k.private)
		if err != nil {
			return nil, fmt.Errorf("token: encode key %s: %w", k.kid, err)
		}
		derived, err := hkdf.Key(sha256.New, secret, nil, purpose, 32)
		if err != nil {
			return nil, fmt.Errorf("token: derive key: %w", err)
		}
		copy(out[i][:], derived)
	}
	return out, nil
}

func algorithmOf(s crypto.Signer) (jose.SignatureAlgorithm, error) {
	switch k := s.(type) {
	case *ecdsa.PrivateKey:
		if k.Curve != elliptic.P256() {
			return "", fmt.Errorf("token: EC keys must use P-256, not %s", k.Curve.Params().Name)
		}
		return jose.ES256, nil
	case ed25519.PrivateKey:
		return jose.EdDSA, nil
	case *rsa.PrivateKey:
		if k.N.BitLen() < minRSABits {
			return "", fmt.Errorf("token: RSA keys need at least %d bits, not %d", minRSABits, k.N.BitLen())
		}
		return jose.RS256, nil
	}
	return "", fmt.Errorf("token: unsupported key type %T", s)
}

func readPrivateKey(file string) (crypto.Signer, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("token: %s holds no PEM block", file)
	}
	var key any
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("token: %s holds a %q, not a private key", file, block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("token: parse %s: %w", file, err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("token: %s holds a key that cannot sign", file)
	}
	return signer, nil
}

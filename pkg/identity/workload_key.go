package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// A workload that holds a key pair, such as a platform workload (ADR 0015),
// authenticates each request with a short JWT it signs itself. The control
// plane keeps only the public key, named by its JWK thumbprint (RFC 7638),
// which the JWT's kid carries. No secret is shared.

const (
	// AudienceControlPlane is the audience of assertions for the control
	// plane. It is a fixed name rather than a URL, because a workload may
	// reach the control plane at an internal address that differs from its
	// external one.
	AudienceControlPlane = "urn:ivaldi:control-plane"

	// MaxWorkloadAssertionLifetime is the longest an assertion may be valid.
	// Assertions are not tracked by ID, so this bounds a replay.
	MaxWorkloadAssertionLifetime = 5 * time.Minute

	// workloadAssertionLifetime is how long the assertions a signer makes
	// are valid.
	workloadAssertionLifetime = 2 * time.Minute

	// workloadAssertionLeeway absorbs clock skew between the two sides.
	workloadAssertionLeeway = 30 * time.Second
)

// CredentialHeader tells the API how to read a bearer token that is not an
// Ivaldi access token or API token.
const CredentialHeader = "X-Ivaldi-Credential"

// Values of CredentialHeader.
const (
	CredentialHeaderKubernetes  = "kubernetes"
	CredentialHeaderWorkloadKey = "workload-key"
)

// workloadKeyAlgorithms are the algorithms a workload key may sign with.
var workloadKeyAlgorithms = []jose.SignatureAlgorithm{jose.EdDSA, jose.ES256}

// WorkloadKeySigner signs a workload's requests with its private key.
type WorkloadKeySigner interface {
	// Sign returns a fresh assertion for audience, valid for two minutes.
	Sign(audience string) (string, error)
	// KeyID is the thumbprint of the signer's public key.
	KeyID() string
}

// NewWorkloadKeySigner returns a signer for a PEM-encoded Ed25519 or ECDSA
// P-256 private key, such as one `openssl genpkey -algorithm ed25519` writes.
func NewWorkloadKeySigner(privateKeyPEM []byte) (WorkloadKeySigner, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	var (
		algorithm jose.SignatureAlgorithm
		public    crypto.PublicKey
	)
	switch k := key.(type) {
	case ed25519.PrivateKey:
		algorithm, public = jose.EdDSA, k.Public()
	case *ecdsa.PrivateKey:
		if k.Curve != elliptic.P256() {
			return nil, errors.New("workload key: an ECDSA key must use P-256")
		}
		algorithm, public = jose.ES256, k.Public()
	default:
		return nil, fmt.Errorf("workload key: %T is not an Ed25519 or ECDSA P-256 key", key)
	}
	keyID, err := WorkloadKeyThumbprint(public)
	if err != nil {
		return nil, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), keyID))
	if err != nil {
		return nil, fmt.Errorf("workload key: %w", err)
	}
	return &workloadKeySigner{signer: signer, keyID: keyID, now: time.Now}, nil
}

type workloadKeySigner struct {
	signer jose.Signer
	keyID  string
	now    func() time.Time
}

func (s *workloadKeySigner) KeyID() string { return s.keyID }

func (s *workloadKeySigner) Sign(audience string) (string, error) {
	if audience == "" {
		return "", errors.New("workload key: an audience is required")
	}
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("workload key: %w", err)
	}
	now := s.now()
	token, err := jwt.Signed(s.signer).Claims(jwt.Claims{
		Audience:  jwt.Audience{audience},
		IssuedAt:  jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(workloadAssertionLifetime)),
		NotBefore: jwt.NewNumericDate(now),
		ID:        base64.RawURLEncoding.EncodeToString(jti),
	}).Serialize()
	if err != nil {
		return "", fmt.Errorf("workload key: sign: %w", err)
	}
	return token, nil
}

// WorkloadKeyAssertion is what a verified assertion says.
type WorkloadKeyAssertion struct {
	// KeyID is the thumbprint of the key that signed it.
	KeyID     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// WorkloadKeyID reads the key ID an assertion names, without verifying it,
// so the verifier can find the public key to verify it with.
func WorkloadKeyID(token string) (string, error) {
	parsed, err := jwt.ParseSigned(token, workloadKeyAlgorithms)
	if err != nil {
		return "", fmt.Errorf("%w: malformed workload assertion", ErrInvalidToken)
	}
	if len(parsed.Headers) != 1 || parsed.Headers[0].KeyID == "" {
		return "", fmt.Errorf("%w: a workload assertion must name its key", ErrInvalidToken)
	}
	return parsed.Headers[0].KeyID, nil
}

// VerifyWorkloadKeyAssertion checks an assertion's signature against public,
// its audience, and its lifetime at now. A key ID that is not public's
// thumbprint is refused, so an assertion cannot borrow another key's name.
func VerifyWorkloadKeyAssertion(token string, public crypto.PublicKey, audience string, now time.Time) (WorkloadKeyAssertion, error) {
	parsed, err := jwt.ParseSigned(token, workloadKeyAlgorithms)
	if err != nil {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: malformed workload assertion", ErrInvalidToken)
	}
	if len(parsed.Headers) != 1 {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: a workload assertion has one signature", ErrInvalidToken)
	}
	thumbprint, err := WorkloadKeyThumbprint(public)
	if err != nil {
		return WorkloadKeyAssertion{}, err
	}
	if parsed.Headers[0].KeyID != thumbprint {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: the assertion names another key", ErrInvalidToken)
	}
	var claims jwt.Claims
	if err := parsed.Claims(public, &claims); err != nil {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: the workload assertion's signature is not valid", ErrInvalidToken)
	}
	if claims.IssuedAt == nil || claims.Expiry == nil {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: a workload assertion needs iat and exp", ErrInvalidToken)
	}
	issued, expires := claims.IssuedAt.Time(), claims.Expiry.Time()
	if expires.Sub(issued) > MaxWorkloadAssertionLifetime {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: a workload assertion may last at most %s", ErrInvalidToken, MaxWorkloadAssertionLifetime)
	}
	err = claims.ValidateWithLeeway(jwt.Expected{AnyAudience: jwt.Audience{audience}, Time: now}, workloadAssertionLeeway)
	switch {
	case errors.Is(err, jwt.ErrExpired):
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: the workload assertion expired", ErrTokenExpired)
	case err != nil:
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if issued.After(now.Add(workloadAssertionLeeway)) {
		return WorkloadKeyAssertion{}, fmt.Errorf("%w: the workload assertion was issued in the future", ErrInvalidToken)
	}
	return WorkloadKeyAssertion{KeyID: thumbprint, IssuedAt: issued, ExpiresAt: expires}, nil
}

// ParseWorkloadPublicKeyPEM reads a PEM-encoded Ed25519 or ECDSA P-256
// public key, such as one `openssl pkey -pubout` writes.
func ParseWorkloadPublicKeyPEM(publicKeyPEM []byte) (crypto.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("workload key: expected a PEM \"PUBLIC KEY\" block")
	}
	return ParseWorkloadPublicKeyDER(block.Bytes)
}

// ParseWorkloadPublicKeyDER reads a DER-encoded (PKIX) workload public key,
// the form the control plane stores.
func ParseWorkloadPublicKeyDER(der []byte) (crypto.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("workload key: %w", err)
	}
	if err := checkWorkloadPublicKey(key); err != nil {
		return nil, err
	}
	return key, nil
}

// MarshalWorkloadPublicKeyDER encodes a workload public key as DER (PKIX).
func MarshalWorkloadPublicKeyDER(public crypto.PublicKey) ([]byte, error) {
	if err := checkWorkloadPublicKey(public); err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return nil, fmt.Errorf("workload key: %w", err)
	}
	return der, nil
}

// WorkloadKeyThumbprint returns the key's JWK SHA-256 thumbprint (RFC 7638),
// base64url-encoded: the name a workload's assertions carry as kid.
func WorkloadKeyThumbprint(public crypto.PublicKey) (string, error) {
	if err := checkWorkloadPublicKey(public); err != nil {
		return "", err
	}
	sum, err := (&jose.JSONWebKey{Key: public}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("workload key: thumbprint: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sum), nil
}

func checkWorkloadPublicKey(public crypto.PublicKey) error {
	switch k := public.(type) {
	case ed25519.PublicKey:
		return nil
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return errors.New("workload key: an ECDSA key must use P-256")
		}
		return nil
	}
	return fmt.Errorf("workload key: %T is not an Ed25519 or ECDSA P-256 key", public)
}

func parsePrivateKey(privateKeyPEM []byte) (any, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, errors.New("workload key: expected a PEM private key")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("workload key: %w", err)
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("workload key: %w", err)
		}
		return key, nil
	}
	return nil, fmt.Errorf("workload key: a %q PEM block is not a private key", block.Type)
}

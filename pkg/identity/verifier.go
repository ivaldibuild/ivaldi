package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Algorithms access tokens may be signed with.
const (
	AlgorithmES256 = "ES256"
	AlgorithmEdDSA = "EdDSA"
	AlgorithmRS256 = "RS256"
)

// AccessTokenType is the typ header of an access token (RFC 9068), which
// keeps an ID token or other JWT signed with the same key from passing as one.
const AccessTokenType = "at+jwt"

var (
	// ErrInvalidToken means the token is malformed, not signed by a trusted
	// key, or not meant for this resource server.
	ErrInvalidToken = errors.New("invalid token")
	// ErrTokenExpired means the token was valid but has expired.
	ErrTokenExpired = errors.New("token expired")
)

// PublicKey is a key that verifies access tokens.
type PublicKey struct {
	KeyID     string
	Algorithm string
	Key       crypto.PublicKey
}

// KeySource returns the keys tokens may be signed with. The control plane
// serves its own; deployables elsewhere fetch the published JWKS.
type KeySource interface {
	PublicKeys(ctx context.Context) ([]PublicKey, error)
}

type staticKeySource []PublicKey

// NewStaticKeySource returns a KeySource that always returns keys.
func NewStaticKeySource(keys ...PublicKey) KeySource { return staticKeySource(keys) }

func (s staticKeySource) PublicKeys(context.Context) ([]PublicKey, error) { return s, nil }

// Verifier checks access tokens offline, against the issuer's keys.
type Verifier interface {
	// Verify returns the token's claims, or an error matching
	// ErrInvalidToken or ErrTokenExpired.
	Verify(ctx context.Context, token string) (*Claims, error)
}

// VerifierConfig says which tokens a Verifier accepts.
type VerifierConfig struct {
	// Issuer is the only iss accepted.
	Issuer string
	// Audience must be in the token's aud: the resource server's own URI.
	Audience string
	Keys     KeySource
	// Leeway allows for clock skew between issuer and verifier, up to a minute.
	Leeway time.Duration
	// Now returns the current time; it defaults to time.Now.
	Now func() time.Time
}

const maxLeeway = time.Minute

type verifier struct {
	cfg VerifierConfig
}

// NewVerifier returns a Verifier, or an error if the configuration is incomplete.
func NewVerifier(cfg VerifierConfig) (Verifier, error) {
	switch {
	case cfg.Issuer == "":
		return nil, errors.New("identity: verifier needs an issuer")
	case cfg.Audience == "":
		return nil, errors.New("identity: verifier needs an audience")
	case cfg.Keys == nil:
		return nil, errors.New("identity: verifier needs keys")
	case cfg.Leeway < 0 || cfg.Leeway > maxLeeway:
		return nil, fmt.Errorf("identity: leeway must be between 0 and %s", maxLeeway)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &verifier{cfg: cfg}, nil
}

var acceptedAlgorithms = []jose.SignatureAlgorithm{jose.ES256, jose.EdDSA, jose.RS256}

func (v *verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	jws, err := jose.ParseSigned(token, acceptedAlgorithms)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if len(jws.Signatures) != 1 {
		return nil, fmt.Errorf("%w: want one signature", ErrInvalidToken)
	}
	header := jws.Signatures[0].Protected
	if typ, _ := header.ExtraHeaders[jose.HeaderType].(string); typ != AccessTokenType {
		return nil, fmt.Errorf("%w: typ %q is not an access token", ErrInvalidToken, typ)
	}

	keys, err := v.cfg.Keys.PublicKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("load verification keys: %w", err)
	}
	var key *PublicKey
	for i := range keys {
		if keys[i].KeyID == header.KeyID {
			key = &keys[i]
			break
		}
	}
	if key == nil {
		return nil, fmt.Errorf("%w: unknown key %q", ErrInvalidToken, header.KeyID)
	}
	// The key decides the algorithm, so a token cannot pick a weaker one.
	if header.Algorithm != key.Algorithm || !algorithmFits(key.Algorithm, key.Key) {
		return nil, fmt.Errorf("%w: algorithm %q does not match key %q", ErrInvalidToken, header.Algorithm, key.KeyID)
	}
	payload, err := jws.Verify(key.Key)
	if err != nil {
		return nil, fmt.Errorf("%w: signature", ErrInvalidToken)
	}

	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrInvalidToken, err)
	}
	if err := v.check(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (v *verifier) check(c *Claims) error {
	now := v.cfg.Now()
	switch {
	case c.Issuer != v.cfg.Issuer:
		return fmt.Errorf("%w: issuer %q", ErrInvalidToken, c.Issuer)
	case !c.HasAudience(v.cfg.Audience):
		return fmt.Errorf("%w: not for %q", ErrInvalidToken, v.cfg.Audience)
	case c.Subject == "" || c.Kind == "":
		return fmt.Errorf("%w: no subject", ErrInvalidToken)
	case c.ExpiresAt.Unix() <= 0:
		return fmt.Errorf("%w: no expiry", ErrInvalidToken)
	case !c.NotBefore.IsZero() && now.Add(v.cfg.Leeway).Before(c.NotBefore):
		return fmt.Errorf("%w: not valid yet", ErrInvalidToken)
	case !now.Add(-v.cfg.Leeway).Before(c.ExpiresAt):
		return ErrTokenExpired
	}
	return nil
}

// algorithmFits reports whether alg is the one a key of this type signs with.
func algorithmFits(alg string, key crypto.PublicKey) bool {
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		return alg == AlgorithmES256 && k.Curve == elliptic.P256()
	case ed25519.PublicKey:
		return alg == AlgorithmEdDSA
	case *rsa.PublicKey:
		return alg == AlgorithmRS256
	}
	return false
}

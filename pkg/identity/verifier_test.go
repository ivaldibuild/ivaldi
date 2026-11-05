package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://ivaldi.example.com"
	testAudience = "https://ivaldi.example.com"
)

var testNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

type testKey struct {
	kid     string
	alg     jose.SignatureAlgorithm
	private crypto.Signer
}

func (k testKey) public() PublicKey {
	return PublicKey{KeyID: k.kid, Algorithm: string(k.alg), Key: k.private.Public()}
}

func newTestKeys(t *testing.T) (ec, ed, rs testKey) {
	t.Helper()
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	rsKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return testKey{"ec", jose.ES256, ecKey}, testKey{"ed", jose.EdDSA, edKey}, testKey{"rs", jose.RS256, rsKey}
}

func validClaims() Claims {
	return Claims{
		Issuer:         testIssuer,
		Subject:        "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Audience:       []string{testAudience},
		IssuedAt:       testNow.Add(-time.Minute),
		NotBefore:      testNow.Add(-time.Minute),
		ExpiresAt:      testNow.Add(14 * time.Minute),
		ID:             "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ClientID:       "ivaldictl",
		Scopes:         []string{"read", "write"},
		Kind:           ActorUser,
	}
}

func sign(t *testing.T, k testKey, typ string, payload any) string {
	t.Helper()
	opts := &jose.SignerOptions{}
	if typ != "" {
		opts = opts.WithType(jose.ContentType(typ))
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: k.alg, Key: jose.JSONWebKey{Key: k.private, KeyID: k.kid}}, opts)
	require.NoError(t, err)
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	jws, err := signer.Sign(b)
	require.NoError(t, err)
	s, err := jws.CompactSerialize()
	require.NoError(t, err)
	return s
}

func newTestVerifier(t *testing.T, keys ...PublicKey) Verifier {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{
		Issuer: testIssuer, Audience: testAudience, Keys: NewStaticKeySource(keys...),
		Leeway: 30 * time.Second, Now: func() time.Time { return testNow },
	})
	require.NoError(t, err)
	return v
}

func TestVerifyAcceptsEveryAlgorithm(t *testing.T) {
	ec, ed, rs := newTestKeys(t)
	v := newTestVerifier(t, ec.public(), ed.public(), rs.public())

	for _, k := range []testKey{ec, ed, rs} {
		t.Run(string(k.alg), func(t *testing.T) {
			got, err := v.Verify(context.Background(), sign(t, k, AccessTokenType, validClaims()))
			require.NoError(t, err)
			want := validClaims()
			assert.Equal(t, &want, got)
			assert.Equal(t, Actor{PrincipalID: want.Subject, Kind: ActorUser}, got.Actor())
		})
	}
}

func TestVerifyRefuses(t *testing.T) {
	ec, _, rs := newTestKeys(t)
	v := newTestVerifier(t, ec.public())
	with := func(change func(*Claims)) Claims {
		c := validClaims()
		change(&c)
		return c
	}

	tests := []struct {
		name  string
		token func(t *testing.T) string
		want  error
	}{
		{"garbage", func(*testing.T) string { return "not.a.token" }, ErrInvalidToken},
		{"no typ", func(t *testing.T) string { return sign(t, ec, "", validClaims()) }, ErrInvalidToken},
		{"an ID token", func(t *testing.T) string { return sign(t, ec, "JWT", validClaims()) }, ErrInvalidToken},
		{"unknown key", func(t *testing.T) string {
			return sign(t, testKey{"other", jose.RS256, rs.private}, AccessTokenType, validClaims())
		}, ErrInvalidToken},
		{"another issuer", func(t *testing.T) string {
			return sign(t, ec, AccessTokenType, with(func(c *Claims) { c.Issuer = "https://evil.example.com" }))
		}, ErrInvalidToken},
		{"another audience", func(t *testing.T) string {
			return sign(t, ec, AccessTokenType, with(func(c *Claims) { c.Audience = []string{"https://gateway.example.com"} }))
		}, ErrInvalidToken},
		{"no subject", func(t *testing.T) string {
			return sign(t, ec, AccessTokenType, with(func(c *Claims) { c.Subject = "" }))
		}, ErrInvalidToken},
		{"not valid yet", func(t *testing.T) string {
			return sign(t, ec, AccessTokenType, with(func(c *Claims) { c.NotBefore = testNow.Add(time.Minute) }))
		}, ErrInvalidToken},
		{"expired", func(t *testing.T) string {
			return sign(t, ec, AccessTokenType, with(func(c *Claims) { c.ExpiresAt = testNow.Add(-time.Minute) }))
		}, ErrTokenExpired},
		{"tampered payload", func(t *testing.T) string {
			parts := strings.Split(sign(t, ec, AccessTokenType, validClaims()), ".")
			other := strings.Split(sign(t, ec, AccessTokenType, with(func(c *Claims) { c.Subject = "prn_someone_else" })), ".")
			return parts[0] + "." + other[1] + "." + parts[2]
		}, ErrInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tt.token(t))
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerifyAllowsLeeway(t *testing.T) {
	ec, _, _ := newTestKeys(t)
	v := newTestVerifier(t, ec.public())
	c := validClaims()
	c.ExpiresAt = testNow.Add(-10 * time.Second)

	_, err := v.Verify(context.Background(), sign(t, ec, AccessTokenType, c))

	assert.NoError(t, err, "a token that expired within the leeway still verifies")
}

func TestVerifyTakesTheAlgorithmFromTheKey(t *testing.T) {
	ec, _, rs := newTestKeys(t)
	// A key published as RS256 must not verify a token its holder signed
	// with another algorithm, nor can a key of one type claim another's.
	mislabeled := PublicKey{KeyID: "ec", Algorithm: AlgorithmRS256, Key: ec.private.Public()}
	v := newTestVerifier(t, mislabeled, rs.public())

	_, err := v.Verify(context.Background(), sign(t, ec, AccessTokenType, validClaims()))

	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestNewVerifierNeedsItsConfig(t *testing.T) {
	keys := NewStaticKeySource()
	tests := map[string]VerifierConfig{
		"no issuer":       {Audience: testAudience, Keys: keys},
		"no audience":     {Issuer: testIssuer, Keys: keys},
		"no keys":         {Issuer: testIssuer, Audience: testAudience},
		"too much leeway": {Issuer: testIssuer, Audience: testAudience, Keys: keys, Leeway: time.Hour},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewVerifier(cfg)
			assert.Error(t, err)
		})
	}
}

func TestClaimsJSON(t *testing.T) {
	b, err := json.Marshal(validClaims())
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"iss": "https://ivaldi.example.com", "sub": "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"aud": ["https://ivaldi.example.com"], "exp": 1793880840, "iat": 1793879940, "nbf": 1793879940,
		"jti": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "org": "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"client_id": "ivaldictl", "scope": "read write", "kind": "user"
	}`, string(b))

	var single Claims
	require.NoError(t, json.Unmarshal([]byte(`{"aud":"https://ivaldi.example.com","exp":1,"iat":1,"kind":"user"}`), &single))
	assert.Equal(t, []string{"https://ivaldi.example.com"}, single.Audience, "aud may be a single string")
	assert.True(t, single.NotBefore.IsZero())
}

func TestCallerContext(t *testing.T) {
	_, ok := CallerFrom(context.Background())
	assert.False(t, ok)

	c := validClaims()
	ctx := WithCaller(context.Background(), CallerOf(&c))

	got, ok := CallerFrom(ctx)
	require.True(t, ok)
	assert.Equal(t, Caller{
		Actor:          Actor{PrincipalID: c.Subject, Kind: ActorUser},
		OrganizationID: c.OrganizationID,
		ClientID:       "ivaldictl",
		Scopes:         []string{"read", "write"},
	}, got)
}

package iam

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

const (
	codeVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeRedirect = "https://app.example.com/callback"
	codeResource = "https://mcp.example.com/"
)

// codeChallenge is the S256 challenge for codeVerifier.
func codeChallenge() string {
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func codeProps() NewOAuthAuthorizationCodeProps {
	return NewOAuthAuthorizationCodeProps{
		CodeHash:       testHash("code"),
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		ClientID:       testClient,
		RedirectURI:    codeRedirect,
		CodeChallenge:  codeChallenge(),
		Resource:       codeResource,
		Scopes:         []string{"write", "read"},
		ExpiresAt:      t1,
	}
}

func codeNew(t *testing.T) *OAuthAuthorizationCode {
	t.Helper()
	c, err := NewOAuthAuthorizationCode(codeProps())
	require.NoError(t, err)
	return c
}

func TestNewOAuthAuthorizationCode(t *testing.T) {
	c := codeNew(t)

	assert.Equal(t, testHash("code"), c.CodeHash())
	assert.Equal(t, testOrg, c.OrganizationID())
	assert.Equal(t, testPrincipal, c.PrincipalID())
	assert.Equal(t, testClient, c.ClientID())
	assert.Equal(t, codeRedirect, c.RedirectURI())
	assert.Equal(t, codeChallenge(), c.CodeChallenge())
	assert.Equal(t, codeResource, c.Resource())
	assert.Equal(t, []string{"read", "write"}, c.Scopes(), "scopes are sorted")
	assert.Equal(t, t1, c.ExpiresAt())
	assert.Nil(t, c.UsedAt())
	assert.False(t, c.Used())
}

func TestNewOAuthAuthorizationCodeWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	props := codeProps()
	props.UsedAt = new(t0)

	c, err := NewOAuthAuthorizationCode(props)

	require.NoError(t, err, "an expired, used code still loads")
	require.NotNil(t, c.UsedAt())
	assert.Equal(t, t0, *c.UsedAt())
	assert.True(t, c.Used())
	assert.True(t, c.Expired())
}

func TestNewOAuthAuthorizationCodeIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewOAuthAuthorizationCodeProps)
	}{
		{"no code hash", func(p *NewOAuthAuthorizationCodeProps) { p.CodeHash = nil }},
		{"short code hash", func(p *NewOAuthAuthorizationCodeProps) { p.CodeHash = []byte("raw-code") }},
		{"no organization", func(p *NewOAuthAuthorizationCodeProps) { p.OrganizationID = "" }},
		{"organization with wrong prefix", func(p *NewOAuthAuthorizationCodeProps) { p.OrganizationID = testWorkspace }},
		{"no principal", func(p *NewOAuthAuthorizationCodeProps) { p.PrincipalID = "" }},
		{"principal with wrong prefix", func(p *NewOAuthAuthorizationCodeProps) { p.PrincipalID = testAccount }},
		{"public client ID instead of ocl_ ID", func(p *NewOAuthAuthorizationCodeProps) { p.ClientID = "https://app.example.com/client.json" }},
		{"no redirect URI", func(p *NewOAuthAuthorizationCodeProps) { p.RedirectURI = "" }},
		{"no code challenge", func(p *NewOAuthAuthorizationCodeProps) { p.CodeChallenge = "" }},
		{"short code challenge", func(p *NewOAuthAuthorizationCodeProps) {
			p.CodeChallenge = strings.Repeat("a", minCodeChallengeChars-1)
		}},
		{"long code challenge", func(p *NewOAuthAuthorizationCodeProps) {
			p.CodeChallenge = strings.Repeat("a", maxCodeChallengeChars+1)
		}},
		{"code challenge with padding", func(p *NewOAuthAuthorizationCodeProps) { p.CodeChallenge = codeChallenge() + "=" }},
		{"no resource", func(p *NewOAuthAuthorizationCodeProps) { p.Resource = "" }},
		{"relative resource", func(p *NewOAuthAuthorizationCodeProps) { p.Resource = "/mcp" }},
		{"resource with fragment", func(p *NewOAuthAuthorizationCodeProps) { p.Resource = codeResource + "#x" }},
		{"empty scope", func(p *NewOAuthAuthorizationCodeProps) { p.Scopes = []string{""} }},
		{"scope with space", func(p *NewOAuthAuthorizationCodeProps) { p.Scopes = []string{"read write"} }},
		{"scope with quote", func(p *NewOAuthAuthorizationCodeProps) { p.Scopes = []string{`re"ad`} }},
		{"duplicate scope", func(p *NewOAuthAuthorizationCodeProps) { p.Scopes = []string{"read", "read"} }},
		{"too many scopes", func(p *NewOAuthAuthorizationCodeProps) {
			p.Scopes = nil
			for i := range maxScopes + 1 {
				p.Scopes = append(p.Scopes, "s"+strings.Repeat("x", i))
			}
		}},
		{"no expiry", func(p *NewOAuthAuthorizationCodeProps) { p.ExpiresAt = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := codeProps()
			tt.mutate(&props)
			_, err := NewOAuthAuthorizationCode(props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestOAuthAuthorizationCodeGettersReturnCopies(t *testing.T) {
	props := codeProps()
	props.UsedAt = new(t0)
	c, err := NewOAuthAuthorizationCode(props)
	require.NoError(t, err)

	c.CodeHash()[0] ^= 0xff
	c.Scopes()[0] = "admin"
	*c.UsedAt() = t2
	snap := c.Snapshot()
	snap.CodeHash[0] ^= 0xff
	snap.Scopes[0] = "admin"
	*snap.UsedAt = t2
	props.CodeHash[0] ^= 0xff
	props.Scopes[0] = "admin"
	*props.UsedAt = t2

	assert.Equal(t, testHash("code"), c.CodeHash())
	assert.Equal(t, []string{"read", "write"}, c.Scopes())
	assert.Equal(t, t0, *c.UsedAt())
}

func TestOAuthAuthorizationCodeSnapshot(t *testing.T) {
	c := codeNew(t)

	assert.Equal(t, OAuthAuthorizationCodeSnapshot{
		CodeHash:       testHash("code"),
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		ClientID:       testClient,
		RedirectURI:    codeRedirect,
		CodeChallenge:  codeChallenge(),
		Resource:       codeResource,
		Scopes:         []string{"read", "write"},
		ExpiresAt:      t1,
	}, c.Snapshot())
}

func TestOAuthAuthorizationCodeRedeem(t *testing.T) {
	clock.Fix(t, t0)
	c := codeNew(t)

	require.NoError(t, c.Redeem(testClient, codeRedirect, codeVerifier))

	require.NotNil(t, c.UsedAt())
	assert.Equal(t, t0, *c.UsedAt())
	assert.True(t, c.Used())

	err := c.Redeem(testClient, codeRedirect, codeVerifier)
	assert.ErrorIs(t, err, ErrAlreadyUsed, "a code is single-use")
	assert.Equal(t, t0, *c.UsedAt())
}

func TestOAuthAuthorizationCodeRedeemIsRefused(t *testing.T) {
	otherClient := "ocl_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	tests := []struct {
		name                         string
		clientID, redirect, verifier string
		want                         error
	}{
		{"another client", otherClient, codeRedirect, codeVerifier, ErrGrantMismatch},
		{"another redirect URI", testClient, "https://app.example.com/other", codeVerifier, ErrGrantMismatch},
		{"wrong verifier", testClient, codeRedirect, strings.Repeat("a", 43), ErrGrantMismatch},
		{"challenge given as verifier", testClient, codeRedirect, codeChallenge(), ErrGrantMismatch},
		{"malformed verifier", testClient, codeRedirect, "short", ErrGrantMismatch},
		{"empty verifier", testClient, codeRedirect, "", ErrGrantMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			c := codeNew(t)

			assert.ErrorIs(t, c.Redeem(tt.clientID, tt.redirect, tt.verifier), tt.want)
			assert.False(t, c.Used(), "a refused redemption does not use the code")
		})
	}

	t.Run("expired", func(t *testing.T) {
		clock.Fix(t, t0)
		c := codeNew(t)
		clock.Fix(t, t1)

		assert.True(t, c.Expired(), "a code is expired at its expiry time")
		assert.ErrorIs(t, c.Redeem(testClient, codeRedirect, codeVerifier), ErrExpired)
		assert.False(t, c.Used())
	})
}

func TestVerifyPKCE(t *testing.T) {
	// The example from RFC 7636, appendix B.
	assert.True(t, verifyPKCE("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"))
	assert.False(t, verifyPKCE("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXl", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"))
}

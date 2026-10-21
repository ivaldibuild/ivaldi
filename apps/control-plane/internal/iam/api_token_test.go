package iam

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func tokenProps() NewAPITokenProps {
	return NewAPITokenProps{
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		Name:           "CI deploys",
		TokenPrefix:    "ivt_a1b2",
		TokenHash:      testHash("ivt_a1b2c3d4"),
	}
}

func tokenNew(t *testing.T, props NewAPITokenProps) *APIToken {
	t.Helper()
	tok, err := NewAPIToken(props, nil)
	require.NoError(t, err)
	return tok
}

func TestNewAPIToken(t *testing.T) {
	clock.Fix(t, t0)

	props := tokenProps()
	props.ExpiresAt = new(t2)
	tok, err := NewAPIToken(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("tok", tok.ID()), "a new token gets a tok_ ID: %q", tok.ID())
	assert.Equal(t, testOrg, tok.OrganizationID())
	assert.Equal(t, testPrincipal, tok.PrincipalID())
	assert.Equal(t, "CI deploys", tok.Name())
	assert.Equal(t, "ivt_a1b2", tok.TokenPrefix())
	assert.Equal(t, testHash("ivt_a1b2c3d4"), tok.TokenHash())
	require.NotNil(t, tok.ExpiresAt())
	assert.Equal(t, t2, *tok.ExpiresAt())
	assert.Nil(t, tok.LastUsedAt())
	assert.Nil(t, tok.RevokedAt())
	assert.Equal(t, t0, tok.CreatedAt())
	assert.False(t, tok.Revoked())
	assert.False(t, tok.Expired())
	assert.NoError(t, tok.CheckUsable())
}

func TestNewAPITokenWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("tok")
	props := tokenProps()
	props.CreatedAt = new(t0)
	props.ExpiresAt = new(t1)
	props.LastUsedAt = new(t0)
	props.RevokedAt = new(t1)

	tok, err := NewAPIToken(props, &given)

	require.NoError(t, err, "a stored token that has expired is still loaded")
	assert.Equal(t, given, tok.ID())
	assert.Equal(t, t0, tok.CreatedAt())
	assert.Equal(t, t0, *tok.LastUsedAt())
	assert.Equal(t, t1, *tok.RevokedAt())
	assert.True(t, tok.Revoked())
	assert.True(t, tok.Expired())
}

func TestNewAPITokenIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewAPITokenProps)
		id     *string
	}{
		{"no organization", func(p *NewAPITokenProps) { p.OrganizationID = "" }, nil},
		{"no principal", func(p *NewAPITokenProps) { p.PrincipalID = "" }, nil},
		{"account as principal", func(p *NewAPITokenProps) { p.PrincipalID = testAccount }, nil},
		{"no name", func(p *NewAPITokenProps) { p.Name = "" }, nil},
		{"name too long", func(p *NewAPITokenProps) { p.Name = strings.Repeat("a", maxNameLength+1) }, nil},
		{"no prefix", func(p *NewAPITokenProps) { p.TokenPrefix = "" }, nil},
		{"prefix too long", func(p *NewAPITokenProps) { p.TokenPrefix = strings.Repeat("a", maxTokenPrefixLength+1) }, nil},
		{"no hash", func(p *NewAPITokenProps) { p.TokenHash = nil }, nil},
		{"short hash", func(p *NewAPITokenProps) { p.TokenHash = []byte("raw-token") }, nil},
		{"expiry at creation", func(p *NewAPITokenProps) { p.CreatedAt = new(t1); p.ExpiresAt = new(t1) }, nil},
		{"expiry before creation", func(p *NewAPITokenProps) { p.CreatedAt = new(t1); p.ExpiresAt = new(t0) }, nil},
		{"zero expiry", func(p *NewAPITokenProps) { p.ExpiresAt = &time.Time{} }, nil},
		{"ID with wrong prefix", func(*NewAPITokenProps) {}, new(id.New("wet"))},
		{"malformed ID", func(*NewAPITokenProps) {}, new("tok_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := tokenProps()
			tt.mutate(&props)
			_, err := NewAPIToken(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAPITokenWithoutExpiryNeverExpires(t *testing.T) {
	clock.Fix(t, t0)
	tok := tokenNew(t, tokenProps())
	clock.Fix(t, t0.AddDate(10, 0, 0))

	assert.Nil(t, tok.ExpiresAt())
	assert.False(t, tok.Expired())
	assert.NoError(t, tok.CheckUsable())
}

func TestAPITokenExpires(t *testing.T) {
	clock.Fix(t, t0)
	props := tokenProps()
	props.ExpiresAt = new(t1)
	tok := tokenNew(t, props)

	clock.Fix(t, t1)

	assert.True(t, tok.Expired(), "a token expires at its expiry time")
	assert.ErrorIs(t, tok.CheckUsable(), ErrExpired)
}

func TestAPITokenRevoke(t *testing.T) {
	clock.Fix(t, t0)
	tok := tokenNew(t, tokenProps())

	clock.Fix(t, t1)
	tok.Revoke()
	assert.True(t, tok.Revoked())
	assert.Equal(t, t1, *tok.RevokedAt())
	assert.ErrorIs(t, tok.CheckUsable(), ErrRevoked)

	clock.Fix(t, t2)
	tok.Revoke()
	assert.Equal(t, t1, *tok.RevokedAt(), "revoking a revoked token does nothing")
}

func TestAPITokenRevokedWinsOverExpired(t *testing.T) {
	clock.Fix(t, t0)
	props := tokenProps()
	props.ExpiresAt = new(t1)
	tok := tokenNew(t, props)
	tok.Revoke()
	clock.Fix(t, t2)

	assert.ErrorIs(t, tok.CheckUsable(), ErrRevoked)
}

func TestAPITokenRecordUse(t *testing.T) {
	clock.Fix(t, t0)
	tok := tokenNew(t, tokenProps())
	clock.Fix(t, t1)

	tok.RecordUse()

	require.NotNil(t, tok.LastUsedAt())
	assert.Equal(t, t1, *tok.LastUsedAt())
}

func TestAPITokenGettersReturnCopies(t *testing.T) {
	props := tokenProps()
	props.ExpiresAt = new(t2)
	props.CreatedAt = new(t0)
	tok := tokenNew(t, props)

	props.TokenHash[0] ^= 0xff
	tok.TokenHash()[0] ^= 0xff
	*tok.ExpiresAt() = t0
	snap := tok.Snapshot()
	snap.TokenHash[0] ^= 0xff
	*snap.ExpiresAt = t0

	assert.Equal(t, testHash("ivt_a1b2c3d4"), tok.TokenHash())
	assert.Equal(t, t2, *tok.ExpiresAt())
}

func TestAPITokenSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	tok := tokenNew(t, tokenProps())

	assert.Equal(t, APITokenSnapshot{
		ID:             tok.ID(),
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		Name:           "CI deploys",
		TokenPrefix:    "ivt_a1b2",
		TokenHash:      testHash("ivt_a1b2c3d4"),
		CreatedAt:      t0,
	}, tok.Snapshot())
}

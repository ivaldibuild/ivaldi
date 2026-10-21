package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const refreshResource = "https://mcp.example.com/"

func refreshProps() NewOAuthRefreshTokenProps {
	return NewOAuthRefreshTokenProps{
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		ClientID:       testClient,
		TokenHash:      testHash("refresh-1"),
		Resource:       refreshResource,
		Scopes:         []string{"write", "read"},
		ExpiresAt:      t2,
	}
}

func refreshNew(t *testing.T) *OAuthRefreshToken {
	t.Helper()
	r, err := NewOAuthRefreshToken(refreshProps(), nil)
	require.NoError(t, err)
	return r
}

func TestNewOAuthRefreshToken(t *testing.T) {
	clock.Fix(t, t0)

	r := refreshNew(t)

	assert.True(t, id.Valid("ort", r.ID()), "a new token gets an ort_ ID: %q", r.ID())
	assert.Equal(t, r.ID(), r.FamilyID(), "a new family is named after its first token")
	assert.Equal(t, testOrg, r.OrganizationID())
	assert.Equal(t, testPrincipal, r.PrincipalID())
	assert.Equal(t, testClient, r.ClientID())
	assert.Equal(t, testHash("refresh-1"), r.TokenHash())
	assert.Equal(t, refreshResource, r.Resource())
	assert.Equal(t, []string{"read", "write"}, r.Scopes(), "scopes are sorted")
	assert.Equal(t, t2, r.ExpiresAt())
	assert.Nil(t, r.ReplacedBy())
	assert.Nil(t, r.RevokedAt())
	assert.Equal(t, t0, r.CreatedAt())
	assert.False(t, r.Replaced())
	assert.False(t, r.Revoked())
	assert.False(t, r.Expired())
}

func TestNewOAuthRefreshTokenWithStoredState(t *testing.T) {
	clock.Fix(t, t2.Add(time.Hour))
	given := id.New("ort")
	family := id.New("ort")
	replacement := id.New("ort")
	props := refreshProps()
	props.FamilyID = family
	props.ReplacedBy = &replacement
	props.RevokedAt = new(t1)
	props.CreatedAt = new(t0)

	r, err := NewOAuthRefreshToken(props, &given)

	require.NoError(t, err, "an expired, revoked token still loads")
	assert.Equal(t, given, r.ID())
	assert.Equal(t, family, r.FamilyID())
	require.NotNil(t, r.ReplacedBy())
	assert.Equal(t, replacement, *r.ReplacedBy())
	require.NotNil(t, r.RevokedAt())
	assert.Equal(t, t1, *r.RevokedAt())
	assert.Equal(t, t0, r.CreatedAt())
	assert.True(t, r.Expired())

	*props.RevokedAt = t2
	assert.Equal(t, t1, *r.RevokedAt(), "the given time is copied")
}

func TestNewOAuthRefreshTokenIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewOAuthRefreshTokenProps)
		id     *string
	}{
		{"no organization", func(p *NewOAuthRefreshTokenProps) { p.OrganizationID = "" }, nil},
		{"no principal", func(p *NewOAuthRefreshTokenProps) { p.PrincipalID = "" }, nil},
		{"principal with wrong prefix", func(p *NewOAuthRefreshTokenProps) { p.PrincipalID = testAccount }, nil},
		{"no client", func(p *NewOAuthRefreshTokenProps) { p.ClientID = "" }, nil},
		{"public client ID", func(p *NewOAuthRefreshTokenProps) { p.ClientID = "my-client" }, nil},
		{"family with wrong prefix", func(p *NewOAuthRefreshTokenProps) { p.FamilyID = testClient }, nil},
		{"no token hash", func(p *NewOAuthRefreshTokenProps) { p.TokenHash = nil }, nil},
		{"raw token", func(p *NewOAuthRefreshTokenProps) { p.TokenHash = []byte("raw-refresh-token") }, nil},
		{"no resource", func(p *NewOAuthRefreshTokenProps) { p.Resource = "" }, nil},
		{"relative resource", func(p *NewOAuthRefreshTokenProps) { p.Resource = "mcp" }, nil},
		{"invalid scope", func(p *NewOAuthRefreshTokenProps) { p.Scopes = []string{"a b"} }, nil},
		{"no expiry", func(p *NewOAuthRefreshTokenProps) { p.ExpiresAt = time.Time{} }, nil},
		{"expiry at creation", func(p *NewOAuthRefreshTokenProps) { p.CreatedAt = new(t1); p.ExpiresAt = t1 }, nil},
		{"expiry before creation", func(p *NewOAuthRefreshTokenProps) { p.CreatedAt = new(t1); p.ExpiresAt = t0 }, nil},
		{"replacement with wrong prefix", func(p *NewOAuthRefreshTokenProps) { p.ReplacedBy = new(testClient) }, nil},
		{"ID with wrong prefix", func(*NewOAuthRefreshTokenProps) {}, new(id.New("ocl"))},
		{"malformed ID", func(*NewOAuthRefreshTokenProps) {}, new("ort_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := refreshProps()
			tt.mutate(&props)
			_, err := NewOAuthRefreshToken(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("empty replacement means none", func(t *testing.T) {
		clock.Fix(t, t0)
		props := refreshProps()
		props.ReplacedBy = new("")
		r, err := NewOAuthRefreshToken(props, nil)
		require.NoError(t, err)
		assert.Nil(t, r.ReplacedBy())
	})
}

func TestOAuthRefreshTokenGettersReturnCopies(t *testing.T) {
	clock.Fix(t, t0)
	props := refreshProps()
	props.RevokedAt = new(t0)
	props.ReplacedBy = new(id.New("ort"))
	r, err := NewOAuthRefreshToken(props, nil)
	require.NoError(t, err)
	replacedBy := *r.ReplacedBy()

	r.TokenHash()[0] ^= 0xff
	r.Scopes()[0] = "admin"
	*r.RevokedAt() = t2
	*r.ReplacedBy() = "ort_other"
	snap := r.Snapshot()
	snap.TokenHash[0] ^= 0xff
	snap.Scopes[0] = "admin"
	*snap.RevokedAt = t2
	*snap.ReplacedBy = "ort_other"
	props.Scopes[0] = "admin"
	props.TokenHash[0] ^= 0xff

	assert.Equal(t, testHash("refresh-1"), r.TokenHash())
	assert.Equal(t, []string{"read", "write"}, r.Scopes())
	assert.Equal(t, t0, *r.RevokedAt())
	assert.Equal(t, replacedBy, *r.ReplacedBy())
}

func TestOAuthRefreshTokenSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	r := refreshNew(t)

	assert.Equal(t, OAuthRefreshTokenSnapshot{
		ID:             r.ID(),
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		ClientID:       testClient,
		FamilyID:       r.ID(),
		TokenHash:      testHash("refresh-1"),
		Resource:       refreshResource,
		Scopes:         []string{"read", "write"},
		ExpiresAt:      t2,
		CreatedAt:      t0,
	}, r.Snapshot())
}

func TestOAuthRefreshTokenRotate(t *testing.T) {
	clock.Fix(t, t0)
	first := refreshNew(t)

	clock.Fix(t, t1)
	second, err := first.Rotate(testClient, testHash("refresh-2"), t2.Add(time.Hour))
	require.NoError(t, err)

	assert.NotEqual(t, first.ID(), second.ID())
	assert.Equal(t, first.FamilyID(), second.FamilyID(), "the replacement joins the same family")
	assert.Equal(t, testOrg, second.OrganizationID())
	assert.Equal(t, testPrincipal, second.PrincipalID())
	assert.Equal(t, testClient, second.ClientID())
	assert.Equal(t, refreshResource, second.Resource())
	assert.Equal(t, first.Scopes(), second.Scopes())
	assert.Equal(t, testHash("refresh-2"), second.TokenHash())
	assert.Equal(t, t2.Add(time.Hour), second.ExpiresAt())
	assert.Equal(t, t1, second.CreatedAt())
	assert.Nil(t, second.ReplacedBy())

	require.NotNil(t, first.ReplacedBy())
	assert.Equal(t, second.ID(), *first.ReplacedBy())
	assert.True(t, first.Replaced())

	third, err := second.Rotate(testClient, testHash("refresh-3"), t2.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, first.ID(), third.FamilyID(), "the family stays named after its first token")
}

func TestOAuthRefreshTokenRotateIsRefused(t *testing.T) {
	t.Run("replayed", func(t *testing.T) {
		clock.Fix(t, t0)
		r := refreshNew(t)
		next, err := r.Rotate(testClient, testHash("refresh-2"), t2)
		require.NoError(t, err)

		_, err = r.Rotate(testClient, testHash("refresh-3"), t2)

		assert.ErrorIs(t, err, ErrAlreadyUsed)
		assert.Equal(t, next.ID(), *r.ReplacedBy(), "a replay changes nothing")
	})

	t.Run("revoked", func(t *testing.T) {
		clock.Fix(t, t0)
		r := refreshNew(t)
		r.Revoke()

		_, err := r.Rotate(testClient, testHash("refresh-2"), t2)

		assert.ErrorIs(t, err, ErrRevoked)
		assert.False(t, r.Replaced())
	})

	t.Run("expired", func(t *testing.T) {
		clock.Fix(t, t0)
		r := refreshNew(t)
		clock.Fix(t, t2)

		_, err := r.Rotate(testClient, testHash("refresh-2"), t2.Add(time.Hour))

		assert.ErrorIs(t, err, ErrExpired)
		assert.False(t, r.Replaced())
	})

	t.Run("another client", func(t *testing.T) {
		clock.Fix(t, t0)
		r := refreshNew(t)

		_, err := r.Rotate("ocl_01BX5ZZKBKACTAV9WEVGEMMVRZ", testHash("refresh-2"), t2)

		assert.ErrorIs(t, err, ErrGrantMismatch)
		assert.False(t, r.Replaced())
	})

	t.Run("invalid replacement", func(t *testing.T) {
		clock.Fix(t, t0)
		r := refreshNew(t)

		_, err := r.Rotate(testClient, []byte("raw"), t2)
		assert.ErrorIs(t, err, ErrInvalid)
		_, err = r.Rotate(testClient, testHash("refresh-2"), t0)
		assert.ErrorIs(t, err, ErrInvalid, "the replacement must expire after now")

		assert.False(t, r.Replaced(), "a refused rotation changes nothing")
	})
}

func TestOAuthRefreshTokenRevoke(t *testing.T) {
	clock.Fix(t, t0)
	r := refreshNew(t)

	clock.Fix(t, t1)
	r.Revoke()
	require.NotNil(t, r.RevokedAt())
	assert.Equal(t, t1, *r.RevokedAt())
	assert.True(t, r.Revoked())

	clock.Fix(t, t2)
	r.Revoke()
	assert.Equal(t, t1, *r.RevokedAt(), "revoking a revoked token does nothing")
}

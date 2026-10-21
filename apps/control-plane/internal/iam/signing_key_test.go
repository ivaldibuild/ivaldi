package iam

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

const keyPublicJWK = `{"kty":"EC","crv":"P-256","x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU","y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}`

func keyProps() NewSigningKeyProps {
	return NewSigningKeyProps{
		KID:            "key-2026-10",
		OrganizationID: testOrg,
		Algorithm:      AlgorithmES256,
		PublicJWK:      json.RawMessage(keyPublicJWK),
		PrivateKeyRef:  "kms://keys/org-signing",
	}
}

func keyNew(t *testing.T) *SigningKey {
	t.Helper()
	k, err := NewSigningKey(keyProps())
	require.NoError(t, err)
	return k
}

func TestNewSigningKey(t *testing.T) {
	clock.Fix(t, t0)

	k := keyNew(t)

	assert.Equal(t, "key-2026-10", k.KID())
	assert.Equal(t, testOrg, k.OrganizationID())
	assert.Equal(t, AlgorithmES256, k.Algorithm())
	assert.JSONEq(t, keyPublicJWK, string(k.PublicJWK()))
	assert.Equal(t, "kms://keys/org-signing", k.PrivateKeyRef())
	assert.Equal(t, SigningKeyActive, k.Status(), "a new key is active")
	assert.Equal(t, t0, k.CreatedAt())
	assert.Nil(t, k.NotAfter())
	assert.True(t, k.Signs())
	assert.True(t, k.Verifies())
}

func TestNewSigningKeyWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	props := keyProps()
	props.Status = SigningKeyRetiring
	props.CreatedAt = new(t0)
	props.NotAfter = new(t1)

	k, err := NewSigningKey(props)

	require.NoError(t, err, "a key past its notAfter still loads")
	assert.Equal(t, SigningKeyRetiring, k.Status())
	assert.Equal(t, t0, k.CreatedAt())
	require.NotNil(t, k.NotAfter())
	assert.Equal(t, t1, *k.NotAfter())

	*props.NotAfter = t2
	assert.Equal(t, t1, *k.NotAfter(), "the given time is copied")
}

func TestNewSigningKeyAcceptsEveryAlgorithmAndStatus(t *testing.T) {
	for _, alg := range []SigningAlgorithm{AlgorithmES256, AlgorithmEdDSA, AlgorithmRS256} {
		props := keyProps()
		props.Algorithm = alg
		_, err := NewSigningKey(props)
		assert.NoError(t, err, string(alg))
	}
	for _, s := range []SigningKeyStatus{SigningKeyActive, SigningKeyRetiring, SigningKeyRetired} {
		props := keyProps()
		props.Status = s
		k, err := NewSigningKey(props)
		require.NoError(t, err, string(s))
		assert.Equal(t, s, k.Status())
	}
}

func TestNewSigningKeyIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewSigningKeyProps)
	}{
		{"no key ID", func(p *NewSigningKeyProps) { p.KID = "" }},
		{"key ID too long", func(p *NewSigningKeyProps) { p.KID = strings.Repeat("k", maxKeyIDLength+1) }},
		{"no organization", func(p *NewSigningKeyProps) { p.OrganizationID = "" }},
		{"organization with wrong prefix", func(p *NewSigningKeyProps) { p.OrganizationID = testWorkspace }},
		{"no algorithm", func(p *NewSigningKeyProps) { p.Algorithm = "" }},
		{"symmetric algorithm", func(p *NewSigningKeyProps) { p.Algorithm = "HS256" }},
		{"no algorithm at all", func(p *NewSigningKeyProps) { p.Algorithm = "none" }},
		{"no public key", func(p *NewSigningKeyProps) { p.PublicJWK = nil }},
		{"public key not JSON", func(p *NewSigningKeyProps) { p.PublicJWK = json.RawMessage("not json") }},
		{"public key not an object", func(p *NewSigningKeyProps) { p.PublicJWK = json.RawMessage(`["kty"]`) }},
		{"public key null", func(p *NewSigningKeyProps) { p.PublicJWK = json.RawMessage(`null`) }},
		{"public key without kty", func(p *NewSigningKeyProps) { p.PublicJWK = json.RawMessage(`{"crv":"P-256"}`) }},
		{"no private key reference", func(p *NewSigningKeyProps) { p.PrivateKeyRef = "" }},
		{"unknown status", func(p *NewSigningKeyProps) { p.Status = "revoked" }},
		{"notAfter at creation", func(p *NewSigningKeyProps) { p.CreatedAt = new(t1); p.NotAfter = new(t1) }},
		{"notAfter before creation", func(p *NewSigningKeyProps) { p.CreatedAt = new(t1); p.NotAfter = new(t0) }},
	}
	for _, member := range privateJWKMembers {
		tests = append(tests, struct {
			name   string
			mutate func(*NewSigningKeyProps)
		}{"private member " + member, func(p *NewSigningKeyProps) {
			p.PublicJWK = json.RawMessage(`{"kty":"EC","` + member + `":"secret"}`)
		}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := keyProps()
			tt.mutate(&props)
			_, err := NewSigningKey(props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestSigningKeyGettersReturnCopies(t *testing.T) {
	clock.Fix(t, t0)
	props := keyProps()
	props.NotAfter = new(t2)
	k, err := NewSigningKey(props)
	require.NoError(t, err)

	k.PublicJWK()[0] = 'X'
	*k.NotAfter() = t0
	snap := k.Snapshot()
	snap.PublicJWK[0] = 'X'
	*snap.NotAfter = t0
	props.PublicJWK[0] = 'X'

	assert.JSONEq(t, keyPublicJWK, string(k.PublicJWK()))
	assert.Equal(t, t2, *k.NotAfter())
}

func TestSigningKeySnapshot(t *testing.T) {
	clock.Fix(t, t0)
	k := keyNew(t)

	assert.Equal(t, SigningKeySnapshot{
		KID:            "key-2026-10",
		OrganizationID: testOrg,
		Algorithm:      AlgorithmES256,
		PublicJWK:      json.RawMessage(keyPublicJWK),
		PrivateKeyRef:  "kms://keys/org-signing",
		Status:         SigningKeyActive,
		CreatedAt:      t0,
	}, k.Snapshot())
}

func TestSigningKeySignsAndVerifies(t *testing.T) {
	tests := []struct {
		name     string
		status   SigningKeyStatus
		notAfter *time.Time
		signs    bool
		verifies bool
	}{
		{"active", SigningKeyActive, nil, true, true},
		{"active before notAfter", SigningKeyActive, new(t2), true, true},
		{"active at notAfter", SigningKeyActive, new(t1), false, false},
		{"retiring", SigningKeyRetiring, new(t2), false, true},
		{"retiring past notAfter", SigningKeyRetiring, new(t1), false, false},
		{"retired", SigningKeyRetired, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := keyProps()
			props.Status = tt.status
			props.NotAfter = tt.notAfter
			k, err := NewSigningKey(props)
			require.NoError(t, err)

			clock.Fix(t, t1)
			assert.Equal(t, tt.signs, k.Signs())
			assert.Equal(t, tt.verifies, k.Verifies())
		})
	}
}

func TestSigningKeyStartRetiring(t *testing.T) {
	clock.Fix(t, t0)
	k := keyNew(t)

	require.NoError(t, k.StartRetiring(t2))

	assert.Equal(t, SigningKeyRetiring, k.Status())
	require.NotNil(t, k.NotAfter())
	assert.Equal(t, t2, *k.NotAfter())
	assert.False(t, k.Signs())
	assert.True(t, k.Verifies())

	require.NoError(t, k.StartRetiring(t1), "retiring a retiring key does nothing")
	assert.Equal(t, t2, *k.NotAfter())
}

func TestSigningKeyStartRetiringIsRefused(t *testing.T) {
	t.Run("not in the future", func(t *testing.T) {
		clock.Fix(t, t1)
		k := keyNew(t)

		assert.ErrorIs(t, k.StartRetiring(t1), ErrInvalid)
		assert.ErrorIs(t, k.StartRetiring(t0), ErrInvalid)
		assert.Equal(t, SigningKeyActive, k.Status())
		assert.Nil(t, k.NotAfter())
	})

	t.Run("retired", func(t *testing.T) {
		clock.Fix(t, t0)
		k := keyNew(t)
		k.Retire()

		assert.ErrorIs(t, k.StartRetiring(t2), ErrStatus)
		assert.Equal(t, SigningKeyRetired, k.Status())
	})
}

func TestSigningKeyRetire(t *testing.T) {
	t.Run("from retiring", func(t *testing.T) {
		clock.Fix(t, t0)
		k := keyNew(t)
		require.NoError(t, k.StartRetiring(t2))

		clock.Fix(t, t1)
		k.Retire()

		assert.Equal(t, SigningKeyRetired, k.Status())
		assert.Equal(t, t1, *k.NotAfter(), "a later notAfter is brought forward to now")
		assert.False(t, k.Verifies())
	})

	t.Run("compromised active key", func(t *testing.T) {
		clock.Fix(t, t0)
		k := keyNew(t)

		k.Retire()

		assert.Equal(t, SigningKeyRetired, k.Status())
		require.NotNil(t, k.NotAfter())
		assert.Equal(t, t0, *k.NotAfter())
		assert.False(t, k.Signs())
	})

	t.Run("keeps an earlier notAfter", func(t *testing.T) {
		clock.Fix(t, t2)
		props := keyProps()
		props.Status = SigningKeyRetiring
		props.CreatedAt = new(t0)
		props.NotAfter = new(t1)
		k, err := NewSigningKey(props)
		require.NoError(t, err)

		k.Retire()

		assert.Equal(t, t1, *k.NotAfter())
	})

	t.Run("retired key", func(t *testing.T) {
		clock.Fix(t, t0)
		k := keyNew(t)
		k.Retire()

		clock.Fix(t, t1)
		k.Retire()

		assert.Equal(t, t0, *k.NotAfter(), "retiring a retired key does nothing")
	})
}

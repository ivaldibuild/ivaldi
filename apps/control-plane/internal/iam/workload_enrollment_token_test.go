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

func wetProps() NewWorkloadEnrollmentTokenProps {
	return NewWorkloadEnrollmentTokenProps{
		OrganizationID:       testOrg,
		TokenHash:            testHash("wet-secret"),
		Pool:                 "eu-west",
		CredentialKind:       CredentialBearerToken,
		CreatedByPrincipalID: testCreator,
		ExpiresAt:            t1,
	}
}

func wetNew(t *testing.T) *WorkloadEnrollmentToken {
	t.Helper()
	tok, err := NewWorkloadEnrollmentToken(wetProps(), nil)
	require.NoError(t, err)
	return tok
}

func wetWorkload(t *testing.T, organizationID, pool string) *Workload {
	t.Helper()
	w, err := NewWorkload(NewWorkloadProps{
		PrincipalProps: PrincipalProps{OrganizationID: organizationID, DisplayName: "worker-1"},
		Component:      ComponentWorker,
		Pool:           pool,
	}, nil)
	require.NoError(t, err)
	return w
}

func TestNewWorkloadEnrollmentToken(t *testing.T) {
	clock.Fix(t, t0)

	tok, err := NewWorkloadEnrollmentToken(wetProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("wet", tok.ID()), "a new token gets a wet_ ID: %q", tok.ID())
	assert.Equal(t, testOrg, tok.OrganizationID())
	assert.Equal(t, testHash("wet-secret"), tok.TokenHash())
	assert.Equal(t, "eu-west", tok.Pool())
	assert.Equal(t, testCreator, tok.CreatedByPrincipalID())
	assert.Equal(t, t1, tok.ExpiresAt())
	assert.Nil(t, tok.UsedAt())
	assert.Nil(t, tok.UsedByPrincipalID())
	assert.Equal(t, t0, tok.CreatedAt())
	assert.False(t, tok.Used())
	assert.False(t, tok.Expired())
}

func TestNewWorkloadEnrollmentTokenWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("wet")
	props := wetProps()
	props.CreatedAt = new(t0)
	props.UsedAt = new(t0.Add(time.Minute))
	props.UsedByPrincipalID = new(testPrincipal)

	tok, err := NewWorkloadEnrollmentToken(props, &given)

	require.NoError(t, err, "a used, expired token is still loaded")
	assert.Equal(t, given, tok.ID())
	assert.Equal(t, t0, tok.CreatedAt())
	assert.Equal(t, t0.Add(time.Minute), *tok.UsedAt())
	assert.Equal(t, testPrincipal, *tok.UsedByPrincipalID())
	assert.True(t, tok.Used())
	assert.True(t, tok.Expired())
}

func TestNewWorkloadEnrollmentTokenIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewWorkloadEnrollmentTokenProps)
		id     *string
	}{
		{"no organization", func(p *NewWorkloadEnrollmentTokenProps) { p.OrganizationID = "" }, nil},
		{"no hash", func(p *NewWorkloadEnrollmentTokenProps) { p.TokenHash = nil }, nil},
		{"short hash", func(p *NewWorkloadEnrollmentTokenProps) { p.TokenHash = []byte("raw") }, nil},
		{"no pool", func(p *NewWorkloadEnrollmentTokenProps) { p.Pool = "" }, nil},
		{"pool too long", func(p *NewWorkloadEnrollmentTokenProps) { p.Pool = strings.Repeat("p", maxPoolLength+1) }, nil},
		{"no creator", func(p *NewWorkloadEnrollmentTokenProps) { p.CreatedByPrincipalID = "" }, nil},
		{"account as creator", func(p *NewWorkloadEnrollmentTokenProps) { p.CreatedByPrincipalID = testAccount }, nil},
		{"no expiry", func(p *NewWorkloadEnrollmentTokenProps) { p.ExpiresAt = time.Time{} }, nil},
		{"expiry at creation", func(p *NewWorkloadEnrollmentTokenProps) { p.ExpiresAt = t0 }, nil},
		{"expiry before creation", func(p *NewWorkloadEnrollmentTokenProps) { p.CreatedAt = new(t2) }, nil},
		{"used without user", func(p *NewWorkloadEnrollmentTokenProps) { p.UsedAt = new(t0) }, nil},
		{"user without use", func(p *NewWorkloadEnrollmentTokenProps) { p.UsedByPrincipalID = new(testPrincipal) }, nil},
		{"empty user with use", func(p *NewWorkloadEnrollmentTokenProps) { p.UsedAt = new(t0); p.UsedByPrincipalID = new("") }, nil},
		{"malformed user", func(p *NewWorkloadEnrollmentTokenProps) { p.UsedAt = new(t0); p.UsedByPrincipalID = new(testAccount) }, nil},
		{"ID with wrong prefix", func(*NewWorkloadEnrollmentTokenProps) {}, new(id.New("tok"))},
		{"malformed ID", func(*NewWorkloadEnrollmentTokenProps) {}, new("wet_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := wetProps()
			tt.mutate(&props)
			_, err := NewWorkloadEnrollmentToken(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestWorkloadEnrollmentTokenRedeem(t *testing.T) {
	clock.Fix(t, t0)
	tok := wetNew(t)
	w := wetWorkload(t, testOrg, "eu-west")
	clock.Fix(t, t0.Add(time.Minute))

	require.NoError(t, tok.Redeem(w))

	assert.True(t, tok.Used())
	assert.Equal(t, t0.Add(time.Minute), *tok.UsedAt())
	assert.Equal(t, w.ID(), *tok.UsedByPrincipalID())
}

func TestWorkloadEnrollmentTokenRedeemIsRefused(t *testing.T) {
	t.Run("already used", func(t *testing.T) {
		clock.Fix(t, t0)
		tok := wetNew(t)
		first := wetWorkload(t, testOrg, "eu-west")
		require.NoError(t, tok.Redeem(first))

		err := tok.Redeem(wetWorkload(t, testOrg, "eu-west"))

		assert.ErrorIs(t, err, ErrAlreadyUsed)
		assert.Equal(t, first.ID(), *tok.UsedByPrincipalID(), "the first use is kept")
	})

	tests := []struct {
		name string
		at   time.Time
		org  string
		pool string
		want error
	}{
		{"expired", t1, testOrg, "eu-west", ErrExpired},
		{"workload in another organization", t0, otherOrg, "eu-west", ErrInvalid},
		{"workload in another pool", t0, testOrg, "us-east", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			tok := wetNew(t)
			w := wetWorkload(t, tt.org, tt.pool)
			clock.Fix(t, tt.at)

			assert.ErrorIs(t, tok.Redeem(w), tt.want)
			assert.False(t, tok.Used(), "nothing changes")
			assert.Nil(t, tok.UsedByPrincipalID())
		})
	}
}

func TestWorkloadEnrollmentTokenGettersReturnCopies(t *testing.T) {
	props := wetProps()
	props.UsedAt = new(t0)
	props.UsedByPrincipalID = new(testPrincipal)
	props.CreatedAt = new(t0.Add(-time.Minute))
	tok, err := NewWorkloadEnrollmentToken(props, nil)
	require.NoError(t, err)

	props.TokenHash[0] ^= 0xff
	*props.UsedByPrincipalID = testCreator
	tok.TokenHash()[0] ^= 0xff
	*tok.UsedAt() = t2
	*tok.UsedByPrincipalID() = testCreator
	snap := tok.Snapshot()
	snap.TokenHash[0] ^= 0xff
	*snap.UsedAt = t2
	*snap.UsedByPrincipalID = testCreator

	assert.Equal(t, testHash("wet-secret"), tok.TokenHash())
	assert.Equal(t, t0, *tok.UsedAt())
	assert.Equal(t, testPrincipal, *tok.UsedByPrincipalID())
}

func TestWorkloadEnrollmentTokenSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	tok := wetNew(t)

	assert.Equal(t, WorkloadEnrollmentTokenSnapshot{
		ID:                   tok.ID(),
		OrganizationID:       testOrg,
		TokenHash:            testHash("wet-secret"),
		Pool:                 "eu-west",
		CredentialKind:       CredentialBearerToken,
		CreatedByPrincipalID: testCreator,
		ExpiresAt:            t1,
		CreatedAt:            t0,
	}, tok.Snapshot())
}

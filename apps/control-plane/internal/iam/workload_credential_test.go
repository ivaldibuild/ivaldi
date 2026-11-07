package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const wcrFingerprint = "sha256:3b1f4c0e9a7d"

func wcrProps() NewWorkloadCredentialProps {
	return NewWorkloadCredentialProps{
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		Kind:           CredentialMTLSCertificate,
		Identifier:     wcrFingerprint,
	}
}

func wcrNew(t *testing.T, props NewWorkloadCredentialProps) *WorkloadCredential {
	t.Helper()
	c, err := NewWorkloadCredential(props, nil)
	require.NoError(t, err)
	return c
}

func TestNewWorkloadCredential(t *testing.T) {
	clock.Fix(t, t0)

	props := wcrProps()
	props.NotAfter = new(t2)
	c, err := NewWorkloadCredential(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("wcr", c.ID()), "a new credential gets a wcr_ ID: %q", c.ID())
	assert.Equal(t, testOrg, c.OrganizationID())
	assert.Equal(t, testPrincipal, c.PrincipalID())
	assert.Equal(t, CredentialMTLSCertificate, c.Kind())
	assert.Equal(t, wcrFingerprint, c.Identifier())
	assert.Equal(t, t2, *c.NotAfter())
	assert.Nil(t, c.RevokedAt())
	assert.Equal(t, t0, c.CreatedAt())
	assert.False(t, c.Revoked())
	assert.False(t, c.Expired())
	assert.NoError(t, c.CheckUsable())
}

func TestNewWorkloadCredentialAcceptsEveryKind(t *testing.T) {
	for _, kind := range []WorkloadCredentialKind{CredentialMTLSCertificate, CredentialKubernetesServiceAccount} {
		t.Run(string(kind), func(t *testing.T) {
			props := wcrProps()
			props.Kind = kind
			c := wcrNew(t, props)
			assert.Equal(t, kind, c.Kind())
		})
	}
}

func TestNewWorkloadCredentialWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("wcr")
	props := wcrProps()
	props.CreatedAt = new(t0)
	props.NotAfter = new(t1)
	props.RevokedAt = new(t1)

	c, err := NewWorkloadCredential(props, &given)

	require.NoError(t, err, "a revoked, expired credential is still loaded")
	assert.Equal(t, given, c.ID())
	assert.Equal(t, t0, c.CreatedAt())
	assert.Equal(t, t1, *c.RevokedAt())
	assert.True(t, c.Revoked())
	assert.True(t, c.Expired())
}

func TestNewWorkloadCredentialIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewWorkloadCredentialProps)
		id     *string
	}{
		{"no organization", func(p *NewWorkloadCredentialProps) { p.OrganizationID = "" }, nil},
		{"no principal", func(p *NewWorkloadCredentialProps) { p.PrincipalID = "" }, nil},
		{"account as principal", func(p *NewWorkloadCredentialProps) { p.PrincipalID = testAccount }, nil},
		{"no kind", func(p *NewWorkloadCredentialProps) { p.Kind = "" }, nil},
		{"unknown kind", func(p *NewWorkloadCredentialProps) { p.Kind = "password" }, nil},
		{"no identifier", func(p *NewWorkloadCredentialProps) { p.Identifier = "" }, nil},
		{"not after at creation", func(p *NewWorkloadCredentialProps) { p.NotAfter = new(t0) }, nil},
		{"not after before creation", func(p *NewWorkloadCredentialProps) { p.CreatedAt = new(t1); p.NotAfter = new(t0) }, nil},
		{"zero not after", func(p *NewWorkloadCredentialProps) { p.NotAfter = &time.Time{} }, nil},
		{"ID with wrong prefix", func(*NewWorkloadCredentialProps) {}, new(id.New("wet"))},
		{"malformed ID", func(*NewWorkloadCredentialProps) {}, new("wcr_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := wcrProps()
			tt.mutate(&props)
			_, err := NewWorkloadCredential(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestWorkloadCredentialWithoutNotAfterNeverExpires(t *testing.T) {
	clock.Fix(t, t0)
	c := wcrNew(t, wcrProps())
	clock.Fix(t, t0.AddDate(10, 0, 0))

	assert.False(t, c.Expired())
	assert.NoError(t, c.CheckUsable())
}

func TestWorkloadCredentialExpires(t *testing.T) {
	clock.Fix(t, t0)
	props := wcrProps()
	props.NotAfter = new(t1)
	c := wcrNew(t, props)

	clock.Fix(t, t1)

	assert.True(t, c.Expired(), "a credential expires at its not-after time")
	assert.ErrorIs(t, c.CheckUsable(), ErrExpired)
}

func TestWorkloadCredentialRevoke(t *testing.T) {
	clock.Fix(t, t0)
	c := wcrNew(t, wcrProps())

	clock.Fix(t, t1)
	c.Revoke()
	assert.True(t, c.Revoked())
	assert.Equal(t, t1, *c.RevokedAt())
	assert.ErrorIs(t, c.CheckUsable(), ErrRevoked)

	clock.Fix(t, t2)
	c.Revoke()
	assert.Equal(t, t1, *c.RevokedAt(), "revoking a revoked credential does nothing")
}

func TestWorkloadCredentialGettersReturnCopies(t *testing.T) {
	props := wcrProps()
	props.CreatedAt = new(t0)
	props.NotAfter = new(t2)
	props.RevokedAt = new(t1)
	c := wcrNew(t, props)

	*props.NotAfter = t0
	*c.NotAfter() = t0
	*c.RevokedAt() = t0
	snap := c.Snapshot()
	*snap.NotAfter = t0
	*snap.RevokedAt = t0

	assert.Equal(t, t2, *c.NotAfter())
	assert.Equal(t, t1, *c.RevokedAt())
}

func TestWorkloadCredentialBearerSnapshotKeepsSecret(t *testing.T) {
	clock.Fix(t, t0)
	props := wcrProps()
	props.Kind = CredentialBearerToken
	props.Identifier = "iwt_display"
	props.SecretHash = testHash("iwt-secret")
	c := wcrNew(t, props)

	snap := c.Snapshot()
	assert.Equal(t, testHash("iwt-secret"), snap.SecretHash)
	assert.Nil(t, snap.ReplacedBy)
}

func TestWorkloadCredentialSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	c := wcrNew(t, wcrProps())

	assert.Equal(t, WorkloadCredentialSnapshot{
		ID:             c.ID(),
		OrganizationID: testOrg,
		PrincipalID:    testPrincipal,
		Kind:           CredentialMTLSCertificate,
		Identifier:     wcrFingerprint,
		CreatedAt:      t0,
	}, c.Snapshot())
}

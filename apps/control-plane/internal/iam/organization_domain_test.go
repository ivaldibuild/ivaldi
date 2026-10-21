package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

func orgDomainProps() NewOrganizationDomainProps {
	return NewOrganizationDomainProps{
		OrganizationID:        testOrg,
		Domain:                "Acme.Example",
		VerificationTokenHash: testHash("txt-token"),
	}
}

func orgDomainNew(t *testing.T) *OrganizationDomain {
	t.Helper()
	d, err := NewOrganizationDomain(orgDomainProps())
	require.NoError(t, err)
	return d
}

func TestNewOrganizationDomain(t *testing.T) {
	clock.Fix(t, t0)

	d, err := NewOrganizationDomain(orgDomainProps())

	require.NoError(t, err)
	assert.Equal(t, testOrg, d.OrganizationID())
	assert.Equal(t, "acme.example", d.Domain(), "the domain is stored in lowercase")
	assert.Equal(t, testHash("txt-token"), d.VerificationTokenHash())
	assert.Nil(t, d.VerifiedAt())
	assert.False(t, d.Verified())
	assert.Equal(t, t0, d.CreatedAt())
}

func TestNewOrganizationDomainWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	props := orgDomainProps()
	props.VerificationTokenHash = nil
	props.VerifiedAt = new(t1)
	props.CreatedAt = new(t0)

	d, err := NewOrganizationDomain(props)

	require.NoError(t, err, "a verified domain needs no token hash")
	require.NotNil(t, d.VerifiedAt())
	assert.Equal(t, t1, *d.VerifiedAt())
	assert.True(t, d.Verified())
	assert.Equal(t, t0, d.CreatedAt())
	assert.Nil(t, d.VerificationTokenHash())

	*props.VerifiedAt = t2
	assert.Equal(t, t1, *d.VerifiedAt(), "the given time is copied")
}

func TestNewOrganizationDomainIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewOrganizationDomainProps)
	}{
		{"no organization", func(p *NewOrganizationDomainProps) { p.OrganizationID = "" }},
		{"organization with wrong prefix", func(p *NewOrganizationDomainProps) { p.OrganizationID = testAccount }},
		{"no domain", func(p *NewOrganizationDomainProps) { p.Domain = "" }},
		{"single label", func(p *NewOrganizationDomainProps) { p.Domain = "localhost" }},
		{"empty label", func(p *NewOrganizationDomainProps) { p.Domain = "acme..example" }},
		{"leading hyphen", func(p *NewOrganizationDomainProps) { p.Domain = "-acme.example" }},
		{"trailing hyphen", func(p *NewOrganizationDomainProps) { p.Domain = "acme-.example" }},
		{"underscore", func(p *NewOrganizationDomainProps) { p.Domain = "ac_me.example" }},
		{"email", func(p *NewOrganizationDomainProps) { p.Domain = "ada@acme.example" }},
		{"label too long", func(p *NewOrganizationDomainProps) { p.Domain = strings.Repeat("a", 64) + ".example" }},
		{"domain too long", func(p *NewOrganizationDomainProps) {
			p.Domain = strings.Repeat(strings.Repeat("a", 60)+".", 5) + "example"
		}},
		{"short token hash", func(p *NewOrganizationDomainProps) { p.VerificationTokenHash = []byte("raw-token") }},
		{"unverified without token", func(p *NewOrganizationDomainProps) { p.VerificationTokenHash = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := orgDomainProps()
			tt.mutate(&props)
			_, err := NewOrganizationDomain(props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestOrganizationDomainMatches(t *testing.T) {
	d := orgDomainNew(t)

	assert.True(t, d.Matches("ada@acme.example"))
	assert.True(t, d.Matches("Ada@ACME.Example"), "matching ignores case")
	assert.False(t, d.Matches("ada@sub.acme.example"), "subdomains do not match")
	assert.False(t, d.Matches("ada@acme.example.org"))
	assert.False(t, d.Matches("acme.example"), "an address needs an @")
}

func TestOrganizationDomainGettersReturnCopies(t *testing.T) {
	props := orgDomainProps()
	d, err := NewOrganizationDomain(props)
	require.NoError(t, err)

	props.VerificationTokenHash[0] ^= 0xff
	d.VerificationTokenHash()[0] ^= 0xff
	snap := d.Snapshot()
	snap.VerificationTokenHash[0] ^= 0xff
	d.Verify()
	*d.VerifiedAt() = t2

	assert.Equal(t, testHash("txt-token"), d.VerificationTokenHash())
	assert.NotEqual(t, t2, *d.VerifiedAt())
}

func TestOrganizationDomainSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	d := orgDomainNew(t)

	assert.Equal(t, OrganizationDomainSnapshot{
		OrganizationID:        testOrg,
		Domain:                "acme.example",
		VerificationTokenHash: testHash("txt-token"),
		CreatedAt:             t0,
	}, d.Snapshot())
}

func TestOrganizationDomainVerify(t *testing.T) {
	clock.Fix(t, t0)
	d := orgDomainNew(t)
	clock.Fix(t, t1)

	d.Verify()

	require.True(t, d.Verified())
	assert.Equal(t, t1, *d.VerifiedAt())
	assert.Equal(t, testHash("txt-token"), d.VerificationTokenHash(), "the token hash is kept for rechecks")

	clock.Fix(t, t2)
	d.Verify()
	assert.Equal(t, t1, *d.VerifiedAt(), "verifying a verified domain does nothing")
}

func TestOrganizationDomainRechallenge(t *testing.T) {
	d := orgDomainNew(t)
	d.Verify()

	require.NoError(t, d.Rechallenge(testHash("new-token")))

	assert.False(t, d.Verified(), "a new challenge needs verifying again")
	assert.Equal(t, testHash("new-token"), d.VerificationTokenHash())
}

func TestOrganizationDomainRechallengeIsRefused(t *testing.T) {
	d := orgDomainNew(t)
	d.Verify()
	before := d.Snapshot()

	assert.ErrorIs(t, d.Rechallenge([]byte("raw-token")), ErrInvalid)
	assert.ErrorIs(t, d.Rechallenge(nil), ErrInvalid)
	assert.Equal(t, before, d.Snapshot(), "nothing changes")
}

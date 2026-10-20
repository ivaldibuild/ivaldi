package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	v, err := ParseVersion("v1.4.0-beta.2+abc123")
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 4, 0}, v.Parts)
	assert.Equal(t, "beta.2", v.PreRelease)
	assert.Equal(t, "abc123", v.Build)
	assert.Equal(t, "v1.4.0-beta.2+abc123", v.String())

	for _, s := range []string{"1", "1.2", "1.2.3.4", "2026.10.20-rc.1"} {
		_, err := ParseVersion(s)
		assert.NoError(t, err, s)
	}
	for _, s := range []string{"", "1.", ".1", "1..2", "01.2", "1.2.3.4.5", "1.x", "1.2-", "1.2-beta..1", "1.2-01", "1.2+", "1.2-beta_1"} {
		_, err := ParseVersion(s)
		assert.ErrorIs(t, err, ErrInvalidVersion, s)
	}
}

func TestVersionCompare(t *testing.T) {
	// Each version is lower than the next, as in the SemVer specification.
	ordered := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.2", "1.10", "2.0.0.1",
	}
	for i := 0; i < len(ordered)-1; i++ {
		a, b := mustVersion(t, ordered[i]), mustVersion(t, ordered[i+1])
		assert.Equal(t, -1, a.Compare(b), "%s < %s", a, b)
		assert.Equal(t, 1, b.Compare(a), "%s > %s", b, a)
	}
	assert.Equal(t, 0, mustVersion(t, "1.4").Compare(mustVersion(t, "1.4.0")), "missing parts are 0")
	assert.Equal(t, 0, mustVersion(t, "1.4.0+a").Compare(mustVersion(t, "v1.4.0+b")), "build metadata and the v prefix are ignored")
}

func TestParseVersionRange(t *testing.T) {
	tests := []struct {
		rng string
		in  []string
		out []string
	}{
		{"1.0", []string{"1.0", "1.0.1", "7.0"}, []string{"0.9", "1.0-beta"}},
		{"[1.0]", []string{"1.0", "1.0.0"}, []string{"1.0.1", "0.9"}},
		{"[1.0,2.0)", []string{"1.0", "1.9.9", "2.0-beta"}, []string{"0.9", "2.0", "2.1"}},
		{"(1.0,)", []string{"1.0.1", "99"}, []string{"1.0", "0.1"}},
		{"(,2.0]", []string{"0.1", "2.0"}, []string{"2.0.1"}},
		{" [ 1.0 , 2.0 ] ", []string{"1.0", "2.0"}, []string{"2.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.rng, func(t *testing.T) {
			r, err := ParseVersionRange(tt.rng)
			require.NoError(t, err)
			for _, s := range tt.in {
				assert.True(t, r.Contains(mustVersion(t, s)), "%s in %s", s, tt.rng)
			}
			for _, s := range tt.out {
				assert.False(t, r.Contains(mustVersion(t, s)), "%s not in %s", s, tt.rng)
			}
		})
	}

	for _, s := range []string{"", "[", "[]", "(1.0)", "[1.0", "1.0]", "(,)", "[1.0,2.0,3.0]", "[2.0,1.0]", "(1.0,1.0]", "[x,2.0]", "[1.0,y]"} {
		_, err := ParseVersionRange(s)
		assert.ErrorIs(t, err, ErrInvalidVersion, s)
	}
}

func mustVersion(t *testing.T, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	require.NoError(t, err, s)
	return v
}

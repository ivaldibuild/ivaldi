package hasher

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cheap keeps tests fast; Verify reads the costs from each hash.
var cheap = Params{Memory: 64, Time: 1, Threads: 1, KeyLength: 32, SaltLength: 16}

func newCheap(t *testing.T, opts ...Option) PasswordHasher {
	t.Helper()
	h, err := NewArgon2id(append([]Option{WithParams(cheap)}, opts...)...)
	require.NoError(t, err)
	return h
}

func TestHashAndVerify(t *testing.T) {
	h := newCheap(t)

	encoded, err := h.Hash("correct horse")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$"), encoded)
	assert.NotContains(t, encoded, "correct horse")

	ok, err := h.Verify("correct horse", encoded)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = h.Verify("Correct horse", encoded)
	require.NoError(t, err)
	assert.False(t, ok, "a wrong password is false, not an error")

	again, err := h.Hash("correct horse")
	require.NoError(t, err)
	assert.NotEqual(t, encoded, again, "every hash has its own salt")
}

func TestVerifyUsesTheCostsInTheHash(t *testing.T) {
	encoded, err := newCheap(t).Hash("pw")
	require.NoError(t, err)

	stronger, err := NewArgon2id(WithParams(Params{Memory: 128, Time: 2, Threads: 2, KeyLength: 32, SaltLength: 16}))
	require.NoError(t, err)
	ok, err := stronger.Verify("pw", encoded)
	require.NoError(t, err)
	assert.True(t, ok, "raising the costs leaves existing hashes valid")
}

func TestVerifyDefaultParams(t *testing.T) {
	h, err := NewArgon2id()
	require.NoError(t, err)
	encoded, err := h.Hash("pw")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=2,p=4$"), encoded)

	ok, err := h.Verify("pw", encoded)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyRefusesMalformedHashes(t *testing.T) {
	h := newCheap(t)
	good, err := h.Hash("pw")
	require.NoError(t, err)
	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}

	tests := map[string]string{
		"empty":             "",
		"bcrypt":            "$2a$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234",
		"argon2i":           with(1, "argon2i"),
		"old version":       with(2, "v=16"),
		"garbled params":    with(3, "m=x,t=1,p=1"),
		"too much memory":   with(3, "m=4194304,t=1,p=1"),
		"too many passes":   with(3, "m=64,t=100,p=1"),
		"too many threads":  with(3, "m=1024,t=1,p=64"),
		"bad salt":          with(4, "!!!"),
		"short salt":        with(4, "YWJj"),
		"bad hash":          with(5, "!!!"),
		"short hash":        with(5, "YWJjZGVmZ2g"),
		"missing a section": strings.Join(parts[:5], "$"),
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify("pw", encoded)
			assert.ErrorIs(t, err, ErrMalformedHash)
			assert.False(t, ok)
		})
	}
}

func TestNewArgon2idRefusesBadParams(t *testing.T) {
	_, err := NewArgon2id(WithParams(Params{Memory: 64, Time: 0, Threads: 1, KeyLength: 32, SaltLength: 16}))
	assert.ErrorIs(t, err, ErrMalformedHash)
}

func TestConcurrentHashingIsBounded(t *testing.T) {
	h := newCheap(t, WithConcurrency(1))

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			encoded, err := h.Hash("pw")
			assert.NoError(t, err)
			ok, err := h.Verify("pw", encoded)
			assert.NoError(t, err)
			assert.True(t, ok)
		})
	}
	wg.Wait()
}

func TestVerifyDummy(t *testing.T) {
	h := newCheap(t)
	assert.NotPanics(t, func() { h.VerifyDummy("anything") })
}

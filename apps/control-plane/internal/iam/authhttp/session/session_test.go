package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

type payload struct {
	AccountID string `json:"acc"`
}

func key(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

func newCodec(t *testing.T, keys ...[32]byte) Codec {
	t.Helper()
	c, err := NewCodec(keys)
	require.NoError(t, err)
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newCodec(t, key(1))
	value, err := c.Encode("signin", payload{AccountID: "acc_1"}, time.Minute)
	require.NoError(t, err)
	assert.NotContains(t, value, "acc_1", "the browser cannot read it")

	var got payload
	require.NoError(t, c.Decode("signin", value, &got))
	assert.Equal(t, "acc_1", got.AccountID)
}

func TestDecodeRefuses(t *testing.T) {
	c := newCodec(t, key(1))
	value, err := c.Encode("signin", payload{AccountID: "acc_1"}, time.Minute)
	require.NoError(t, err)
	tampered := []byte(value)
	tampered[len(tampered)/2] ^= 1

	tests := map[string]func() error{
		"tampered":           func() error { return c.Decode("signin", string(tampered), &payload{}) },
		"under another name": func() error { return c.Decode("csrf", value, &payload{}) },
		"not base64":         func() error { return c.Decode("signin", "!!!", &payload{}) },
		"too short":          func() error { return c.Decode("signin", "YWJj", &payload{}) },
		"another key":        func() error { return newCodec(t, key(2)).Decode("signin", value, &payload{}) },
	}
	for name, decode := range tests {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, decode(), ErrInvalid)
		})
	}
}

func TestDecodeRefusesExpired(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	c := newCodec(t, key(1))
	value, err := c.Encode("signin", payload{}, time.Minute)
	require.NoError(t, err)

	clock.Fix(t, now.Add(time.Minute))

	assert.ErrorIs(t, c.Decode("signin", value, &payload{}), ErrInvalid)
}

func TestRotationKeepsOldCookiesReadable(t *testing.T) {
	old := newCodec(t, key(1))
	value, err := old.Encode("signin", payload{AccountID: "acc_1"}, time.Minute)
	require.NoError(t, err)

	rotated := newCodec(t, key(2), key(1))

	var got payload
	require.NoError(t, rotated.Decode("signin", value, &got))
	assert.Equal(t, "acc_1", got.AccountID)
}

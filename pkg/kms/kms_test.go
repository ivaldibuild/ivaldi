package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A data key wrapped with the local key unwraps only with it, and data
// sealed with the data key opens only for the record it was sealed for.
func TestEnvelope(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kek")
	require.NoError(t, os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))+"\n"), 0o600))
	kek, err := LoadLocal(path)
	require.NoError(t, err)

	dataKey, err := NewDataKey()
	require.NoError(t, err)
	wrapped, err := kek.Wrap(ctx, dataKey)
	require.NoError(t, err)
	assert.NotContains(t, string(wrapped.Ciphertext), string(dataKey))
	unwrapped, err := kek.Unwrap(ctx, wrapped)
	require.NoError(t, err)
	assert.Equal(t, dataKey, unwrapped)

	other, err := NewLocal(bytes.Repeat([]byte{8}, 32))
	require.NoError(t, err)
	_, err = other.Unwrap(ctx, wrapped)
	assert.ErrorIs(t, err, ErrUnknownKey, "another key-encryption key")

	sealed, err := Seal(dataKey, []byte("s3cret"), []byte("org_a/sst_a"))
	require.NoError(t, err)
	opened, err := Open(dataKey, sealed, []byte("org_a/sst_a"))
	require.NoError(t, err)
	assert.Equal(t, "s3cret", string(opened))
	_, err = Open(dataKey, sealed, []byte("org_a/sst_b"))
	assert.ErrorIs(t, err, ErrDecrypt, "sealed for another record")

	_, err = NewLocal([]byte("short"))
	assert.Error(t, err)
}

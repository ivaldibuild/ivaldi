package kms

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// keySize is an AES-256 key's size.
const keySize = 32

// NewLocal returns an encryptor whose key-encryption key is key, 32 bytes.
// Its KeyID is derived from the key, so a wrapped data key says which key
// wrapped it without revealing it.
func NewLocal(key []byte) (KeyEncryptor, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("kms: a local key-encryption key is %d bytes, not %d", keySize, len(key))
	}
	sum := sha256.Sum256(key)
	return &local{key: append([]byte(nil), key...), id: "local:" + hex.EncodeToString(sum[:8])}, nil
}

// LoadLocal reads a local key-encryption key from a file: 32 bytes, or
// their base64 or hex encoding.
func LoadLocal(path string) (KeyEncryptor, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kms: read the key-encryption key: %w", err)
	}
	if len(raw) == keySize {
		return NewLocal(raw)
	}
	text := strings.TrimSpace(string(raw))
	if key, err := hex.DecodeString(text); err == nil && len(key) == keySize {
		return NewLocal(key)
	}
	if key, err := base64.StdEncoding.DecodeString(text); err == nil && len(key) == keySize {
		return NewLocal(key)
	}
	return nil, fmt.Errorf("kms: %s does not hold a %d-byte key, raw or as base64 or hex", path, keySize)
}

type local struct {
	key []byte
	id  string
}

func (l *local) Wrap(_ context.Context, dataKey []byte) (Wrapped, error) {
	sealed, err := Seal(l.key, dataKey, []byte(l.id))
	if err != nil {
		return Wrapped{}, err
	}
	return Wrapped{KeyID: l.id, Ciphertext: sealed}, nil
}

func (l *local) Unwrap(_ context.Context, w Wrapped) ([]byte, error) {
	if w.KeyID != l.id {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, w.KeyID)
	}
	return Open(l.key, w.Ciphertext, []byte(l.id))
}

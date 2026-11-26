// Package kms is ADR 0004's key-management interface: a key-encryption key
// wraps the data keys that encrypt tenant data, so the data is unreadable
// without the key-management service. The key-encryption key never leaves
// it; only wrapped data keys are stored, next to the data.
//
// The local implementation keeps the key-encryption key in a file, for
// installations without an external key-management service. AWS KMS and
// Vault Transit implement the same interface when a customer needs their
// own key.
package kms

import (
	"context"
	"errors"
)

// KeyEncryptor wraps data keys with a key-encryption key and unwraps them.
type KeyEncryptor interface {
	// Wrap encrypts a data key with the current key-encryption key.
	Wrap(ctx context.Context, dataKey []byte) (Wrapped, error)
	// Unwrap decrypts a data key wrapped with any key this encryptor holds.
	// It fails with ErrUnknownKey for a key it does not hold, and with
	// ErrDecrypt for a wrapped key that does not open.
	Unwrap(ctx context.Context, w Wrapped) ([]byte, error)
}

// Wrapped is a data key encrypted with the key-encryption key KeyID names.
type Wrapped struct {
	KeyID      string
	Ciphertext []byte
}

var (
	// ErrUnknownKey means a wrapped data key names a key-encryption key the
	// encryptor does not hold, such as one rotated out.
	ErrUnknownKey = errors.New("kms: unknown key-encryption key")
	// ErrDecrypt means a ciphertext does not open with its key: it was
	// changed, or sealed for other associated data.
	ErrDecrypt = errors.New("kms: cannot decrypt")
)

// Package session seals small values into cookies the browser holds but
// cannot read or change, such as who signed in to the authorization server
// and the state of a sign-in in progress at an identity provider.
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// KeyPurpose names the keys cookies are sealed with, when they are derived
// from other secrets, such as the token signing keys.
const KeyPurpose = "ivaldi/authhttp/cookies/v1"

// ErrInvalid means a cookie was tampered with, sealed by a key no longer
// held, sealed under another name, or has expired.
var ErrInvalid = errors.New("session: invalid or expired cookie")

// Codec seals values into cookie values and opens them again.
type Codec interface {
	// Encode seals v, readable as name until ttl passes.
	Encode(name string, v any, ttl time.Duration) (string, error)
	// Decode opens a value Encode sealed under name into v, or returns
	// ErrInvalid.
	Decode(name, value string, v any) error
}

type sealed struct {
	Expires int64           `json:"exp"`
	Value   json.RawMessage `json:"v"`
}

type codec struct {
	aeads []cipher.AEAD
}

// NewCodec returns a Codec using AES-256-GCM. The first key seals; every key
// opens, so values sealed before a key rotation stay readable. The cookie's
// name is bound in as associated data, so one cookie cannot be replayed as
// another.
func NewCodec(keys [][32]byte) (Codec, error) {
	if len(keys) == 0 {
		return nil, errors.New("session: no keys")
	}
	c := &codec{}
	for _, k := range keys {
		block, err := aes.NewCipher(k[:])
		if err != nil {
			return nil, fmt.Errorf("session: %w", err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("session: %w", err)
		}
		c.aeads = append(c.aeads, aead)
	}
	return c, nil
}

func (c *codec) Encode(name string, v any, ttl time.Duration) (string, error) {
	value, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("session: encode: %w", err)
	}
	plain, err := json.Marshal(sealed{Expires: clock.Now().Add(ttl).Unix(), Value: value})
	if err != nil {
		return "", fmt.Errorf("session: encode: %w", err)
	}
	aead := c.aeads[0]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("session: nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte(name))), nil
}

func (c *codec) Decode(name, value string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ErrInvalid
	}
	for _, aead := range c.aeads {
		if len(raw) < aead.NonceSize() {
			return ErrInvalid
		}
		plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(name))
		if err != nil {
			continue
		}
		var s sealed
		if err := json.Unmarshal(plain, &s); err != nil {
			return ErrInvalid
		}
		if clock.Now().Unix() >= s.Expires {
			return ErrInvalid
		}
		if err := json.Unmarshal(s.Value, v); err != nil {
			return ErrInvalid
		}
		return nil
	}
	return ErrInvalid
}

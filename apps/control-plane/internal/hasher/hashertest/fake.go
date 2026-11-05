// Package hashertest provides a fast PasswordHasher for tests.
package hashertest

import (
	"encoding/base64"
	"strings"
	"sync"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher"
)

// fakePrefix keeps the argon2id prefix the iam domain requires of a stored
// hash; the rest is the password, base64-encoded, so tests stay fast.
const fakePrefix = "$argon2id$v=19$m=8,t=1,p=1$ZmFrZQ$"

// Fake is a PasswordHasher that does no hashing. It counts its checks, so
// tests can see that a sign-in for an unknown account still spends one.
type Fake struct {
	mu       sync.Mutex
	verifies int
	dummies  int
}

var _ hasher.PasswordHasher = (*Fake)(nil)

// NewFake returns a Fake.
func NewFake() *Fake { return &Fake{} }

func (f *Fake) Hash(password string) (string, error) {
	return fakePrefix + base64.RawStdEncoding.EncodeToString([]byte(password)), nil
}

func (f *Fake) Verify(password, encoded string) (bool, error) {
	f.mu.Lock()
	f.verifies++
	f.mu.Unlock()
	rest, ok := strings.CutPrefix(encoded, fakePrefix)
	if !ok {
		return false, hasher.ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil {
		return false, hasher.ErrMalformedHash
	}
	return string(want) == password, nil
}

func (f *Fake) VerifyDummy(string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dummies++
}

// Checks returns how many times Verify and VerifyDummy were called.
func (f *Fake) Checks() (verifies, dummies int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verifies, f.dummies
}

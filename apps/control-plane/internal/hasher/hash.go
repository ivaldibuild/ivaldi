// Package hasher hashes passwords with argon2id and checks them against the
// stored hash. Callers depend on PasswordHasher, so tests can use the fast
// fake in hashertest instead of spending memory and time on argon2.
package hasher

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordHasher hashes passwords and checks them.
type PasswordHasher interface {
	// Hash returns the password's argon2id hash in PHC format, such as
	// "$argon2id$v=19$m=65536,t=2,p=4$<salt>$<hash>".
	Hash(password string) (string, error)
	// Verify reports whether the password matches the encoded hash. A wrong
	// password is false; a hash it cannot read is ErrMalformedHash.
	Verify(password, encoded string) (bool, error)
	// VerifyDummy spends the time Verify would, for a sign-in to an account
	// that does not exist or has no password, so how long a sign-in takes
	// does not reveal which accounts exist.
	VerifyDummy(password string)
}

// ErrMalformedHash means a stored hash is not an argon2id hash this package
// can check, or asks for more resources than it allows.
var ErrMalformedHash = errors.New("malformed password hash")

// Params are argon2id's costs. Memory is in KiB.
type Params struct {
	Memory     uint32
	Time       uint32
	Threads    uint8
	KeyLength  uint32
	SaltLength uint32
}

// DefaultParams follow RFC 9106's second recommended option, with two passes.
var DefaultParams = Params{Memory: 64 * 1024, Time: 2, Threads: 4, KeyLength: 32, SaltLength: 16}

// Limits on the costs Verify accepts from a stored hash, so a tampered or
// foreign hash cannot make one check use unbounded memory or time.
const (
	maxMemory    = 1 << 20 // 1 GiB, in KiB
	maxTime      = 10
	maxThreads   = 16
	maxKeyLength = 64
	minKeyLength = 16
	maxSaltBytes = 64
	minSaltBytes = 8
)

// Option configures NewArgon2id.
type Option func(*argon2id)

// WithParams sets the costs of new hashes. Verify uses the costs recorded in
// each hash, so changing them leaves existing hashes valid.
func WithParams(p Params) Option {
	return func(h *argon2id) { h.params = p }
}

// WithConcurrency bounds how many hashes are computed at once. Each takes
// Params.Memory, so without a bound a burst of sign-ins could exhaust the
// process's memory; the rest wait their turn.
func WithConcurrency(n int) Option {
	return func(h *argon2id) { h.slots = make(chan struct{}, max(n, 1)) }
}

type argon2id struct {
	params Params
	slots  chan struct{}
	dummy  string
}

// NewArgon2id returns an argon2id PasswordHasher. By default it uses
// DefaultParams and computes at most twice as many hashes at once as the
// process has CPUs.
func NewArgon2id(opts ...Option) (PasswordHasher, error) {
	h := &argon2id{params: DefaultParams}
	WithConcurrency(2 * runtime.GOMAXPROCS(0))(h)
	for _, o := range opts {
		o(h)
	}
	if err := checkParams(h.params.Memory, h.params.Time, h.params.Threads, h.params.KeyLength, h.params.SaltLength); err != nil {
		return nil, err
	}
	dummy, err := h.Hash(rand.Text())
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

func (h *argon2id) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := h.derive(password, salt, h.params.Time, h.params.Memory, h.params.Threads, h.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.Memory, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (h *argon2id) Verify(password, encoded string) (bool, error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, err
	}
	got := h.derive(password, salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func (h *argon2id) VerifyDummy(password string) {
	_, _ = h.Verify(password, h.dummy)
}

// derive computes the key, waiting for a free slot first.
func (h *argon2id) derive(password string, salt []byte, time, memory uint32, threads uint8, keyLength uint32) []byte {
	h.slots <- struct{}{}
	defer func() { <-h.slots }()
	return argon2.IDKey([]byte(password), salt, time, memory, threads, keyLength)
}

// decode parses a PHC-format argon2id hash and checks its costs.
func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("%w: not an argon2id hash", ErrMalformedHash)
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("%w: unsupported version %q", ErrMalformedHash, parts[2])
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: parameters %q", ErrMalformedHash, parts[3])
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: salt", ErrMalformedHash)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: hash", ErrMalformedHash)
	}
	if err := checkParams(p.Memory, p.Time, p.Threads, uint32(len(key)), uint32(len(salt))); err != nil {
		return Params{}, nil, nil, err
	}
	return p, salt, key, nil
}

func checkParams(memory, time uint32, threads uint8, keyLength, saltLength uint32) error {
	switch {
	case threads < 1 || threads > maxThreads:
		return fmt.Errorf("%w: %d threads", ErrMalformedHash, threads)
	case time < 1 || time > maxTime:
		return fmt.Errorf("%w: %d passes", ErrMalformedHash, time)
	case memory < 8*uint32(threads) || memory > maxMemory:
		return fmt.Errorf("%w: %d KiB of memory", ErrMalformedHash, memory)
	case keyLength < minKeyLength || keyLength > maxKeyLength:
		return fmt.Errorf("%w: %d-byte key", ErrMalformedHash, keyLength)
	case saltLength < minSaltBytes || saltLength > maxSaltBytes:
		return fmt.Errorf("%w: %d-byte salt", ErrMalformedHash, saltLength)
	}
	return nil
}

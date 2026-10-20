// Package id generates the ULIDs Ivaldi uses as primary keys.
//
// IDs are a domain concern: a capability's domain code assigns the ID when it
// creates an object, and repositories only persist it.
//
// A domain type that declares a Prefix gets prefixed IDs: a three-letter type
// prefix, an underscore, and a ULID, such as wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV.
// The prefix makes an ID's type legible in logs and URLs. Every other type
// gets plain ULIDs, such as 01ARZ3NDEKTSV4RRFFQ69G5FAV. Both fit varchar(30)
// columns, and IDs of one type sort by creation time.
//
// A type that declares a prefix registers it from an init function, which
// keeps prefixes unique across the capabilities in one program: a prefix
// claimed twice panics when the program starts. Programs do not see each
// other's registrations, so a prefix used by two deployables is not caught.
package id

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"
)

// Prefixed is implemented by domain types whose IDs carry a type prefix.
type Prefixed interface {
	Prefix() string
}

var (
	prefixFormat = regexp.MustCompile(`^[a-z]{3}$`)
	ulidFormat   = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

	mu     sync.RWMutex
	owners = map[string]string{}
)

// Register records that owner, such as a table name, uses prefix. It fails
// if the prefix is not three lowercase letters or another owner has it.
// Registering the same prefix for the same owner again does nothing.
func Register(prefix, owner string) error {
	if !prefixFormat.MatchString(prefix) {
		return fmt.Errorf("id prefix %q for %s must be three lowercase letters", prefix, owner)
	}
	mu.Lock()
	defer mu.Unlock()
	if o, ok := owners[prefix]; ok && o != owner {
		return fmt.Errorf("id prefix %q for %s is already used by %s", prefix, owner, o)
	}
	owners[prefix] = owner
	return nil
}

// MustRegister is Register that panics on failure, for init functions.
func MustRegister(prefix, owner string) {
	if err := Register(prefix, owner); err != nil {
		panic(err)
	}
}

// New returns a new ID with the given prefix. It panics if the prefix was
// never registered, which is a programming error.
func New(prefix string) string {
	mu.RLock()
	_, ok := owners[prefix]
	mu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("id prefix %q is not registered", prefix))
	}
	return prefix + "_" + ULID()
}

// ULID returns a new ULID with no prefix.
func ULID() string {
	return ulid.Make().String()
}

// For returns a new ID for v: prefixed if it implements Prefixed, and a
// plain ULID otherwise.
func For(v any) string {
	if p, ok := v.(Prefixed); ok {
		return New(p.Prefix())
	}
	return ULID()
}

// Assign sets *dst to a new ID for v if it is empty, and leaves an existing
// ID alone. Domain code calls it when it creates an object:
//
//	w := &Workspace{OrganizationID: org, ...}
//	id.Assign(&w.ID, w)
func Assign(dst *string, v any) {
	if *dst == "" {
		*dst = For(v)
	}
}

// Valid reports whether s is a well-formed ID with the given prefix.
func Valid(prefix, s string) bool {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	return ok && prefixFormat.MatchString(prefix) && ulidFormat.MatchString(rest)
}

// ValidULID reports whether s is a plain ULID with no prefix.
func ValidULID(s string) bool {
	return ulidFormat.MatchString(s)
}

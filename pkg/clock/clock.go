// Package clock is the domain's source of the current time.
//
// Domain objects never take a time as an argument; they read Now, so every
// timestamp a capability records is in UTC at the precision PostgreSQL stores.
package clock

import (
	"testing"
	"time"
)

// clock is replaced only by Fix.
var clock = time.Now

// Now returns the current time as the domain records it: in UTC, truncated to
// the microsecond precision PostgreSQL stores, so an object compares equal
// before and after it is saved and loaded.
func Now() time.Time {
	return clock().UTC().Truncate(time.Microsecond)
}

// Fix makes Now report at until the test ends. Tests that call it must not
// run in parallel, because the clock is shared by the whole program.
func Fix(t testing.TB, at time.Time) {
	t.Helper()
	previous := clock
	clock = func() time.Time { return at }
	t.Cleanup(func() { clock = previous })
}

// CopyTime returns a copy of t, so callers cannot change a domain object
// through a pointer it handed out or was given.
func CopyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

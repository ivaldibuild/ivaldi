package workspace

import "time"

// clock is the domain's source of the current time. Tests replace it to fix
// the time; nothing else should.
var clock = time.Now

// now returns the current time as the domain records it: in UTC, truncated to
// the microsecond precision PostgreSQL stores, so an object compares equal
// before and after it is saved and loaded.
func now() time.Time {
	return clock().UTC().Truncate(time.Microsecond)
}

package versioncontrol

import "time"

// Provider events can be handled out of order: the gateway may publish
// redeliveries late, and the consumer handles several events at once. A
// Source or Connection therefore keeps the provider's timestamp of the newest
// event it applied, and an event older than that changes nothing. The
// timestamp moves even when the event changed nothing else, so an older event
// that would have changed something is still refused: an unarchive handled
// before the archive it followed keeps the repository unarchived.
//
// Equal timestamps are not stale, because providers report seconds and two
// different events can share one; they apply in the order they are handled.
// A zero timestamp means the provider gave none: the event applies and the
// stored timestamp stays.

// providerTime normalizes a provider timestamp like the clock does, so it
// compares equal after a database round trip.
func providerTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Microsecond)
}

// staleProviderEvent reports whether an event at at is older than the newest
// one applied.
func staleProviderEvent(newest *time.Time, at time.Time) bool {
	return !at.IsZero() && newest != nil && at.Before(*newest)
}

// newestProviderEvent returns the newest applied timestamp after applying an
// event at at.
func newestProviderEvent(newest *time.Time, at time.Time) *time.Time {
	if at.IsZero() || (newest != nil && !at.After(*newest)) {
		return newest
	}
	return &at
}

package outbox

import "time"

// Defaults for a Relay that leaves a setting unset.
const (
	DefaultBatch       = 100
	DefaultInterval    = time.Second
	DefaultLease       = 30 * time.Second
	DefaultMaxAttempts = 10
	DefaultBackoff     = time.Second
	DefaultMaxBackoff  = 5 * time.Minute
	DefaultRetention   = 7 * 24 * time.Hour

	// purgeInterval is how often Run removes published entries when
	// Retention is set.
	purgeInterval = time.Hour
)

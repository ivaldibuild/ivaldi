package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNowIsUTCAtMicrosecondPrecision(t *testing.T) {
	bst := time.FixedZone("BST", 60*60)
	fixClock(t, time.Date(2026, 10, 20, 8, 37, 16, 381760612, bst))

	got := now()

	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t, time.Date(2026, 10, 20, 7, 37, 16, 381760000, time.UTC), got, "converted to UTC and truncated to microseconds, as PostgreSQL stores it")
}

func TestNowDropsTheMonotonicReading(t *testing.T) {
	got := now()

	// Round(0) strips a monotonic clock reading; a time without one is unchanged.
	assert.Equal(t, got, got.Round(0), "times without a monotonic reading compare equal after a database round trip")
}

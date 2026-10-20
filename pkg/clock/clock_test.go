package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNowIsUTCAtMicrosecondPrecision(t *testing.T) {
	bst := time.FixedZone("BST", 60*60)
	Fix(t, time.Date(2026, 10, 20, 8, 37, 16, 381760612, bst))

	got := Now()

	assert.Equal(t, time.UTC, got.Location())
	assert.Equal(t, time.Date(2026, 10, 20, 7, 37, 16, 381760000, time.UTC), got, "converted to UTC and truncated to microseconds, as PostgreSQL stores it")
}

func TestNowDropsTheMonotonicReading(t *testing.T) {
	got := Now()

	// Round(0) strips a monotonic clock reading; a time without one is unchanged.
	assert.Equal(t, got, got.Round(0), "times without a monotonic reading compare equal after a database round trip")
}

func TestFixIsUndoneWhenTheTestEnds(t *testing.T) {
	at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("fixed", func(t *testing.T) {
		Fix(t, at)
		assert.Equal(t, at, Now())
	})
	assert.NotEqual(t, at, Now())
}

func TestCopyTime(t *testing.T) {
	assert.Nil(t, CopyTime(nil))

	original := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	c := CopyTime(&original)
	*c = c.Add(time.Hour)
	assert.Equal(t, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), original, "changing the copy leaves the original alone")
}

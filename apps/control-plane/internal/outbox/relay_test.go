package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// fakeStore is an in-memory Store. It hands claimed entries out once, as a
// lease would until it expires, and remembers what the relay recorded.
type fakeStore struct {
	mu           sync.Mutex
	pending      []Entry
	published    []string
	failed       map[string]time.Time
	deadLettered map[string]string
	purged       []time.Time
	claimErr     error
}

func newFakeStore(entries ...Entry) *fakeStore {
	return &fakeStore{
		pending:      entries,
		failed:       map[string]time.Time{},
		deadLettered: map[string]string{},
	}
}

// Record makes fakeStore an outbox.Store; the relay tests never write.
func (s *fakeStore) Record(context.Context, ...events.Envelope) error { return nil }

func (s *fakeStore) Claim(_ context.Context, _ time.Time, limit int, _ time.Duration) ([]Entry, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.pending) {
		limit = len(s.pending)
	}
	out := append([]Entry(nil), s.pending[:limit]...)
	s.pending = s.pending[limit:]
	return out, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, _ time.Time, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published = append(s.published, ids...)
	return nil
}

func (s *fakeStore) MarkFailed(_ context.Context, _ time.Time, id string, retryAt time.Time, _ error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[id] = retryAt
	return nil
}

func (s *fakeStore) DeadLetter(_ context.Context, _ time.Time, id string, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadLettered[id] = cause.Error()
	return nil
}

func (s *fakeStore) PurgePublished(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purged = append(s.purged, before)
	return 0, nil
}

func entry(id, eventType string, attempts int) Entry {
	return Entry{
		Envelope: events.Envelope{ID: id, Type: eventType},
		Attempts: attempts,
	}
}

func newTestRelay(t *testing.T, store Store, handler events.Handler, tune func(*RelayConfig)) *Relay {
	t.Helper()
	c := RelayConfig{Store: store, Handler: handler}
	if tune != nil {
		tune(&c)
	}
	r, err := NewRelay(c)
	require.NoError(t, err)
	return r
}

func TestDrainDispatchesInOrderAndMarksPublished(t *testing.T) {
	clock.Fix(t, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	store := newFakeStore(entry("evt_1", "test.v1.first", 0), entry("evt_2", "test.v1.second", 0))
	var handled []string
	relay := newTestRelay(t, store, events.HandlerFunc(func(_ context.Context, e events.Envelope) error {
		handled = append(handled, e.ID)
		return nil
	}), nil)

	n, err := relay.Drain(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{"evt_1", "evt_2"}, handled)
	assert.Equal(t, []string{"evt_1", "evt_2"}, store.published)
}

func TestDrainSchedulesRetryAfterAFailure(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	store := newFakeStore(entry("evt_1", "test.v1.first", 0))
	handlerErr := errors.New("handler failed")
	relay := newTestRelay(t, store, events.HandlerFunc(func(context.Context, events.Envelope) error {
		return handlerErr
	}), func(c *RelayConfig) { c.Backoff = 2 * time.Second })

	n, err := relay.Drain(context.Background())

	assert.Equal(t, 1, n)
	assert.ErrorIs(t, err, handlerErr)
	assert.Empty(t, store.published)
	assert.Equal(t, now.Add(2*time.Second), store.failed["evt_1"])
}

func TestDrainDeadLettersAfterMaxAttempts(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	store := newFakeStore(entry("evt_1", "test.v1.first", 2))
	handlerErr := errors.New("still failing")
	relay := newTestRelay(t, store, events.HandlerFunc(func(context.Context, events.Envelope) error {
		return handlerErr
	}), func(c *RelayConfig) { c.MaxAttempts = 3 })

	_, err := relay.Drain(context.Background())

	assert.ErrorIs(t, err, handlerErr)
	assert.Empty(t, store.published)
	assert.Empty(t, store.failed)
	assert.Equal(t, "still failing", store.deadLettered["evt_1"])
}

func TestDrainContinuesAfterAFailure(t *testing.T) {
	clock.Fix(t, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	store := newFakeStore(entry("evt_1", "test.v1.bad", 0), entry("evt_2", "test.v1.good", 0))
	handlerErr := errors.New("bad event")
	relay := newTestRelay(t, store, events.HandlerFunc(func(_ context.Context, e events.Envelope) error {
		if e.ID == "evt_1" {
			return handlerErr
		}
		return nil
	}), nil)

	n, err := relay.Drain(context.Background())

	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, handlerErr)
	assert.Equal(t, []string{"evt_2"}, store.published)
	assert.Contains(t, store.failed, "evt_1")
}

func TestDrainReturnsAClaimError(t *testing.T) {
	store := newFakeStore()
	store.claimErr = errors.New("database down")
	relay := newTestRelay(t, store, events.HandlerFunc(func(context.Context, events.Envelope) error {
		return nil
	}), nil)

	n, err := relay.Drain(context.Background())

	assert.Zero(t, n)
	assert.ErrorContains(t, err, "database down")
}

func TestNewRelayRequiresStoreAndHandler(t *testing.T) {
	tests := []struct {
		name string
		c    RelayConfig
		want string
	}{
		{"no store", RelayConfig{Handler: events.HandlerFunc(func(context.Context, events.Envelope) error { return nil })}, "Store is required"},
		{"no handler", RelayConfig{Store: newFakeStore()}, "Handler is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRelay(tt.c)
			assert.Nil(t, r)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNewRelayAppliesDefaults(t *testing.T) {
	relay := newTestRelay(t, newFakeStore(), events.HandlerFunc(func(context.Context, events.Envelope) error {
		return nil
	}), nil)

	assert.Equal(t, DefaultBatch, relay.batch)
	assert.Equal(t, DefaultInterval, relay.interval)
	assert.Equal(t, DefaultLease, relay.lease)
	assert.Equal(t, DefaultMaxAttempts, relay.maxAttempts)
	assert.Equal(t, DefaultBackoff, relay.backoff)
	assert.Equal(t, DefaultMaxBackoff, relay.maxBackoff)
	assert.Equal(t, DefaultRetention, relay.retention)
}

func TestNewRelayRejectsInvalidSettings(t *testing.T) {
	tests := []struct {
		name string
		tune func(*RelayConfig)
		want string
	}{
		{"negative batch", func(c *RelayConfig) { c.Batch = -1 }, "Batch is invalid"},
		{"negative interval", func(c *RelayConfig) { c.Interval = -1 }, "Interval is invalid"},
		{"negative lease", func(c *RelayConfig) { c.Lease = -1 }, "Lease is invalid"},
		{"negative attempts", func(c *RelayConfig) { c.MaxAttempts = -1 }, "MaxAttempts is invalid"},
		{"negative backoff", func(c *RelayConfig) { c.Backoff = -1 }, "Backoff is invalid"},
		{"max backoff below backoff", func(c *RelayConfig) { c.Backoff = time.Minute; c.MaxBackoff = time.Second }, "MaxBackoff is invalid"},
		{"negative retention", func(c *RelayConfig) { c.Retention = -1 }, "Retention is invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := RelayConfig{Store: newFakeStore(), Handler: events.HandlerFunc(func(context.Context, events.Envelope) error { return nil })}
			tt.tune(&c)
			r, err := NewRelay(c)
			assert.Nil(t, r)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestDelay(t *testing.T) {
	relay := newTestRelay(t, newFakeStore(), events.HandlerFunc(func(context.Context, events.Envelope) error {
		return nil
	}), func(c *RelayConfig) {
		c.Backoff = time.Second
		c.MaxBackoff = 5 * time.Second
	})

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 5 * time.Second},
		{10, 5 * time.Second},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, relay.delay(tt.attempts), "attempts=%d", tt.attempts)
	}
}

func TestRunReturnsWhenTheContextEnds(t *testing.T) {
	relay := newTestRelay(t, newFakeStore(), events.HandlerFunc(func(context.Context, events.Envelope) error {
		return nil
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, relay.Run(ctx), context.Canceled)
}

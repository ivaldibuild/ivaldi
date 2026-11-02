package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

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

// RelayConfig holds a Relay's collaborators and tuning. Store and Handler are
// required; every other field has a default.
type RelayConfig struct {
	Store Store
	// Handler receives each claimed event. A Router dispatches it to the
	// handlers in this process; events.Forward publishes it to another
	// process.
	Handler events.Handler
	// Logger reports transient failures. It may be nil, in which case the
	// Relay is silent; Drain still returns the error.
	Logger logging.Logger

	// Batch is the most entries one claim takes. Default DefaultBatch.
	Batch int
	// Interval is how long Run waits when the outbox is empty, or after a
	// failed drain. Default DefaultInterval.
	Interval time.Duration
	// Lease is how long a claim is exclusive. It must cover dispatching a
	// full batch, so a slow handler may make it necessary to raise the lease
	// or lower Batch. Default DefaultLease.
	Lease time.Duration
	// MaxAttempts is how many failed deliveries an event gets before it is
	// dead-lettered. Default DefaultMaxAttempts.
	MaxAttempts int
	// Backoff is the delay after the first failure; it doubles with every
	// later one up to MaxBackoff. Default DefaultBackoff.
	Backoff time.Duration
	// MaxBackoff caps the retry delay. Default DefaultMaxBackoff.
	MaxBackoff time.Duration
	// Retention is how long published entries are kept before Run purges
	// them. Default DefaultRetention.
	Retention time.Duration
	// DisablePurge stops Run from deleting published entries, for a caller
	// that archives the table itself.
	DisablePurge bool
}

// Relay claims stored events and dispatches them through a Handler.
type Relay struct {
	store        Store
	handler      events.Handler
	logger       logging.Logger
	batch        int
	interval     time.Duration
	lease        time.Duration
	maxAttempts  int
	backoff      time.Duration
	maxBackoff   time.Duration
	retention    time.Duration
	disablePurge bool
}

// NewRelay returns a Relay, or an error naming a missing collaborator or an
// invalid setting.
func NewRelay(c RelayConfig) (*Relay, error) {
	if c.Store == nil {
		return nil, errors.New("outbox relay: Store is required")
	}
	if c.Handler == nil {
		return nil, errors.New("outbox relay: Handler is required")
	}
	r := &Relay{
		store:        c.Store,
		handler:      c.Handler,
		logger:       c.Logger,
		batch:        c.Batch,
		interval:     c.Interval,
		lease:        c.Lease,
		maxAttempts:  c.MaxAttempts,
		backoff:      c.Backoff,
		maxBackoff:   c.MaxBackoff,
		retention:    c.Retention,
		disablePurge: c.DisablePurge,
	}
	if r.batch == 0 {
		r.batch = DefaultBatch
	}
	if r.interval == 0 {
		r.interval = DefaultInterval
	}
	if r.lease == 0 {
		r.lease = DefaultLease
	}
	if r.maxAttempts == 0 {
		r.maxAttempts = DefaultMaxAttempts
	}
	if r.backoff == 0 {
		r.backoff = DefaultBackoff
	}
	if r.maxBackoff == 0 {
		r.maxBackoff = DefaultMaxBackoff
	}
	if r.retention == 0 {
		r.retention = DefaultRetention
	}

	invalid := []struct {
		name string
		bad  bool
	}{
		{"Batch", r.batch < 0},
		{"Interval", r.interval < 0},
		{"Lease", r.lease < 0},
		{"MaxAttempts", r.maxAttempts < 0},
		{"Backoff", r.backoff < 0},
		{"MaxBackoff", r.maxBackoff < r.backoff},
		{"Retention", r.retention < 0},
	}
	for _, v := range invalid {
		if v.bad {
			return nil, fmt.Errorf("outbox relay: %s is invalid", v.name)
		}
	}
	return r, nil
}

// Drain claims one batch and dispatches it in claimed order. A handler
// failure is recorded for retry and returned, but does not stop the rest of
// the batch. A nonzero-length error therefore means at least one event was
// not delivered.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	entries, err := r.store.Claim(ctx, clock.Now(), r.batch, r.lease)
	if err != nil {
		return 0, fmt.Errorf("claim outbox events: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if err := r.dispatch(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return len(entries), errors.Join(errs...)
}

// dispatch hands one entry to the handler and records what happened. A
// handler that succeeds but cannot be marked published is redelivered after
// its lease expires, which is why handlers must be idempotent.
func (r *Relay) dispatch(ctx context.Context, e Entry) error {
	if err := r.handler.Handle(ctx, e.Envelope); err != nil {
		if err := r.fail(ctx, e, err); err != nil {
			return err
		}
		return fmt.Errorf("handle event %s (%s): %w", e.Envelope.ID, e.Envelope.Type, err)
	}
	if err := r.store.MarkPublished(ctx, clock.Now(), []string{e.Envelope.ID}); err != nil {
		return fmt.Errorf("mark event %s published: %w", e.Envelope.ID, err)
	}
	return nil
}

// fail records a failed delivery, dead-lettering the event once it has had
// maxAttempts failures.
func (r *Relay) fail(ctx context.Context, e Entry, cause error) error {
	now := clock.Now()
	attempts := e.Attempts + 1
	if attempts >= r.maxAttempts {
		if err := r.store.DeadLetter(ctx, now, e.Envelope.ID, cause); err != nil {
			return fmt.Errorf("dead-letter event %s: %w", e.Envelope.ID, err)
		}
		r.logError("outbox relay: event dead-lettered", e, cause)
		return nil
	}
	retryAt := now.Add(r.delay(attempts))
	if err := r.store.MarkFailed(ctx, now, e.Envelope.ID, retryAt, cause); err != nil {
		return fmt.Errorf("record failure of event %s: %w", e.Envelope.ID, err)
	}
	r.logError("outbox relay: event will be retried", e, cause)
	return nil
}

// delay returns the retry delay after the given number of failures.
func (r *Relay) delay(attempts int) time.Duration {
	d := r.backoff
	for i := 1; i < attempts; i++ {
		if d >= r.maxBackoff/2 {
			return r.maxBackoff
		}
		d *= 2
	}
	if d > r.maxBackoff {
		return r.maxBackoff
	}
	return d
}

func (r *Relay) logError(msg string, e Entry, err error) {
	if r.logger == nil {
		return
	}
	r.logger.Error(msg,
		logging.F("event_id", e.Envelope.ID),
		logging.F("event_type", e.Envelope.Type),
		logging.F("attempts", e.Attempts+1),
		logging.F("error", err.Error()),
	)
}

// Run drains the outbox until ctx is cancelled, catching up without waiting
// while batches stay full and purging published entries hourly. It returns
// the reason the context ended, normally context.Canceled.
func (r *Relay) Run(ctx context.Context) error {
	lastPurge := clock.Now()
	for {
		n, err := r.Drain(ctx)
		if err != nil {
			r.logDrainError(err)
		}
		if !r.disablePurge && clock.Now().Sub(lastPurge) >= purgeInterval {
			if _, err := r.store.PurgePublished(ctx, clock.Now().Add(-r.retention)); err != nil {
				r.logDrainError(fmt.Errorf("purge published events: %w", err))
			}
			lastPurge = clock.Now()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && n == r.batch {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.interval):
		}
	}
}

func (r *Relay) logDrainError(err error) {
	if r.logger == nil {
		return
	}
	r.logger.Error("outbox relay: drain failed", logging.F("error", err.Error()))
}

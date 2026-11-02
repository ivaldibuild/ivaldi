// Package outbox durably records the domain events services raise and hands
// them to consumers later.
//
// A service wraps the events a domain object recorded (see pkg/events) and
// calls Recorder.Record inside the transaction that saves the object, so an
// event exists only if its change committed. A Relay then claims stored
// envelopes and dispatches them, marking each published only after its
// handler returns. Delivery is at least once, and handlers must be
// idempotent.
//
// The package owns the control-plane database table that holds the events.
// It lives in the control plane rather than pkg/ because only the control
// plane touches that database (ADR 0005); the transport it dispatches to is
// an events.Handler or events.Publisher from pkg/events.
package outbox

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// Recorder stores envelopes in the caller's transaction. A service calls it
// after saving the object whose events it carries, so the two commit
// together.
type Recorder interface {
	Record(ctx context.Context, envelopes ...events.Envelope) error
}

// Store is the outbox table. Recorder writes events in a service's
// transaction; the Relay claims and marks them.
type Store interface {
	Recorder
	// Claim leases up to limit pending entries to this caller until now+lease.
	// Entries that are published, dead-lettered, waiting to be retried, or
	// leased by another caller are left out.
	Claim(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]Entry, error)
	// MarkPublished records that the entries were dispatched and hands their
	// leases back.
	MarkPublished(ctx context.Context, now time.Time, ids []string) error
	// MarkFailed records a failed delivery and when it may be claimed again.
	MarkFailed(ctx context.Context, now time.Time, id string, retryAt time.Time, cause error) error
	// DeadLetter records that an entry will not be retried.
	DeadLetter(ctx context.Context, now time.Time, id string, cause error) error
	// PurgePublished deletes entries published before the cutoff and returns
	// how many it deleted.
	PurgePublished(ctx context.Context, before time.Time) (int64, error)
}

// Entry is one claimed event and how many deliveries have failed before.
type Entry struct {
	Envelope events.Envelope
	// Attempts is the number of failed deliveries already recorded for the
	// event, so a handler failure makes it Attempts+1.
	Attempts int
}

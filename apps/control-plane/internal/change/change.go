// Package change is how a capability's services write: a change commits the
// object, the events it raised, and its audit record in one transaction.
// Authorization happens before, in each service's authorizing wrapper, which
// passes its decision down in the context; a change refuses to write an audit
// record without one, so a write that skipped authorization fails rather than
// going through. Capabilities built like delivery (an authorizing wrapper
// around a service that does not authorize) write through it.
package change

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// ErrUnchanged is what an Apply returns when there is nothing to change:
// Run then saves nothing and records nothing.
var ErrUnchanged = errors.New("unchanged")

// ErrUnauthorized means a change reached its audit record with no
// authorization decision for it: a service was called without its
// authorizing wrapper.
var ErrUnauthorized = errors.New("change was not authorized")

// Aggregate is a domain aggregate: it belongs to an Organization and holds
// the events its methods recorded until they are saved.
type Aggregate interface {
	OrganizationID() string
	PullEvents() []aggregate.Event
}

// Writer commits changes: the transaction, the outbox for events, and the
// audit log.
type Writer struct {
	Tx     transaction.Transactor
	Outbox outbox.Recorder
	Audit  audit.Recorder
}

// Check reports a missing dependency, for a service's constructor.
func (w Writer) Check() error {
	switch {
	case w.Tx == nil:
		return errors.New("a Transactor is required")
	case w.Outbox == nil:
		return errors.New("an Outbox Recorder is required")
	case w.Audit == nil:
		return errors.New("an Audit Recorder is required")
	}
	return nil
}

// Events writes the events obj recorded to the outbox, in the caller's
// transaction, so they commit with the object they describe. aggregateID is
// the object's ID, or its key when it has no ID of its own.
func (w Writer) Events(ctx context.Context, aggregateType, aggregateID string, obj Aggregate) error {
	envelopes, err := events.Wrap(ctx, events.Source{
		OrganizationID: obj.OrganizationID(),
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
	}, obj.PullEvents()...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", aggregateType, err)
	}
	if err := w.Outbox.Record(ctx, envelopes...); err != nil {
		return fmt.Errorf("record %s events: %w", aggregateType, err)
	}
	return nil
}

// AuditRecord writes an audit record stamped with the decision that allowed
// it: the context's decision for the record's resource, or for the call.
func (w Writer) AuditRecord(ctx context.Context, e audit.Event) error {
	d, ok := authorization.DecisionFor(ctx, e.ResourceID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnauthorized, e.Type)
	}
	e.DecisionID = d.ID
	e.ApprovedByPrincipalID = d.ApprovedByPrincipalID
	e.OccurredAt = clock.Now()
	if err := w.Audit.Record(ctx, e); err != nil {
		return fmt.Errorf("record %s: %w", e.Type, err)
	}
	return nil
}

// Spec is one change to a stored object.
type Spec[T Aggregate] struct {
	// AggregateType names the domain type that raises the events.
	AggregateType string
	// AggregateID names the changed object for the events.
	AggregateID func(T) string
	// Load reads the object inside the transaction, locking it when the
	// change checks rules other writers could break meanwhile.
	Load func(ctx context.Context) (T, error)
	// Apply changes the object through its methods, and checks rules that
	// span other records. Returning ErrUnchanged skips saving.
	Apply    func(ctx context.Context, current T) error
	Save     func(ctx context.Context, current T) error
	Snapshot func(T) any
	// Event is the audit record, without its actor and snapshots.
	Event func(current T) audit.Event
}

// Run applies the change in a transaction: it loads the object, applies the
// change, saves it, and records its events and audit record, with the
// object's state before and after.
func Run[T Aggregate](ctx context.Context, w Writer, actor identity.Actor, s Spec[T]) (T, error) {
	var current T
	err := w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if current, err = s.Load(ctx); err != nil {
			return err
		}
		before := s.Snapshot(current)
		if err := s.Apply(ctx, current); err != nil {
			return err
		}
		if err := s.Save(ctx, current); err != nil {
			return err
		}
		if err := w.Events(ctx, s.AggregateType, s.AggregateID(current), current); err != nil {
			return err
		}
		e := s.Event(current)
		e.Actor = actor
		e.Before = before
		e.After = s.Snapshot(current)
		return w.AuditRecord(ctx, e)
	})
	if errors.Is(err, ErrUnchanged) {
		return current, nil
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return current, nil
}

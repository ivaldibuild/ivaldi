// Package events carries domain events from the objects that raise them to
// the handlers that react to them.
//
// A service wraps the events a domain object recorded (see pkg/aggregate) in
// Envelopes and saves them in the transaction that saves the object. In the
// control plane, the outbox package stores them and its Relay hands each to
// the handlers subscribed to its type through a Router. Sagas are handlers:
// they invoke other functionality, or forward events to a transport through a
// Publisher, whose implementation in pkg/events/redisstream adds them to a
// stream for handlers in other processes.
//
// A Consumer reads events back from a transport and hands each as a Delivery
// to a DeliveryHandler. The transport acknowledges a delivery only after its
// handler says the work is done, and the handler must be idempotent because
// delivery is at least once and may be out of order.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// prefix is the type prefix of event IDs.
const prefix = "evt"

func init() {
	id.MustRegister(prefix, "events.Envelope")
}

// Envelope carries an event with what handlers need to route, deduplicate,
// and trace it. Its payload is JSON, so an envelope survives an outbox table
// or a stream unchanged.
type Envelope struct {
	// ID identifies the event. Handlers use it to recognize an event they
	// already handled.
	ID string
	// Type is the event's name, including its contract version.
	Type           string
	OrganizationID string
	// AggregateType and AggregateID name the domain object that raised the
	// event. They are empty for events from outside the domain, such as the
	// gateway's.
	AggregateType string
	AggregateID   string
	OccurredAt    time.Time
	// CorrelationID is shared by every event in one chain of reactions: it is
	// the ID of the event that started the chain.
	CorrelationID string
	// CausationID is the ID of the event whose handling raised this one, and
	// empty for an event that started a chain.
	CausationID string
	Payload     json.RawMessage
}

// Source names the domain object that raised events, and its Organization.
type Source struct {
	OrganizationID string
	AggregateType  string
	AggregateID    string
}

// Wrap puts events raised by one source in envelopes, in order. If ctx
// carries a cause (see WithCause), the envelopes continue its chain;
// otherwise each starts its own.
func Wrap(ctx context.Context, src Source, events ...aggregate.Event) ([]Envelope, error) {
	c, caused := causeOf(ctx)
	envelopes := make([]Envelope, 0, len(events))
	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("encode event %s: %w", e.EventName(), err)
		}
		env := Envelope{
			ID:             id.New(prefix),
			Type:           e.EventName(),
			OrganizationID: src.OrganizationID,
			AggregateType:  src.AggregateType,
			AggregateID:    src.AggregateID,
			OccurredAt:     clock.Now(),
			Payload:        payload,
		}
		if caused {
			env.CorrelationID = c.correlationID
			env.CausationID = c.id
		} else {
			env.CorrelationID = env.ID
		}
		envelopes = append(envelopes, env)
	}
	return envelopes, nil
}

// Decode returns e's payload as a T. It fails if e does not carry a T.
func Decode[T aggregate.Event](e Envelope) (T, error) {
	var event T
	if name := event.EventName(); e.Type != name {
		return event, fmt.Errorf("event %s is a %s, not a %s", e.ID, e.Type, name)
	}
	if err := json.Unmarshal(e.Payload, &event); err != nil {
		return event, fmt.Errorf("decode event %s (%s): %w", e.ID, e.Type, err)
	}
	return event, nil
}

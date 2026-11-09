package events

import "context"

// Delivery is one event as delivered by a transport, such as a stream entry.
// It carries the transport's identity and delivery count, and it acknowledges
// itself.
type Delivery interface {
	// Envelope is the event the delivery carries.
	Envelope() Envelope
	// EntryID identifies the delivery in its transport. It is not the event
	// ID: a transport may deliver the same event more than once.
	EntryID() string
	// DeliveryCount is how many times the transport has delivered this entry,
	// including the current delivery.
	DeliveryCount() int64
	// Ack acknowledges the delivery. It is safe to call more than once.
	Ack(ctx context.Context) error
}

// DeliveryHandler handles one delivered event. A consumer that requires
// explicit acknowledgement leaves the delivery unacknowledged until the
// handler calls Delivery.Ack.
type DeliveryHandler interface {
	HandleDelivery(ctx context.Context, d Delivery) error
}

// DeliveryHandlerFunc adapts a function to a DeliveryHandler.
type DeliveryHandlerFunc func(ctx context.Context, d Delivery) error

func (f DeliveryHandlerFunc) HandleDelivery(ctx context.Context, d Delivery) error {
	return f(ctx, d)
}

// Consumer reads envelopes from a transport until its context is done.
// Delivery is at least once and may be out of order, so handlers must be
// idempotent. A consumer acknowledges a delivery only after its handler says
// the work is done; see Delivery and DeliveryHandler.
type Consumer interface {
	Run(ctx context.Context) error
}

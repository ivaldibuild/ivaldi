package events

import "context"

// Publisher sends envelopes to a transport, such as a Redis stream, for
// handlers in other processes. Domain code sees this interface, never the
// transport.
type Publisher interface {
	Publish(ctx context.Context, envelopes ...Envelope) error
}

// Forward returns a handler that publishes every event it handles, for
// subscribing a transport to the events other processes need.
func Forward(p Publisher) Handler {
	return HandlerFunc(func(ctx context.Context, e Envelope) error {
		return p.Publish(ctx, e)
	})
}

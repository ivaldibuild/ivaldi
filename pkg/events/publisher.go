package events

import (
	"context"
	"errors"
)

// ErrUnavailable marks a failure of the transport rather than of the events:
// the connection is down, the server refused the command, or the context
// ended. The events themselves are fine, so the caller retries them later
// without counting the failure against them. Publishers wrap their transport
// errors with it, and a Handler may return it for the same reason.
var ErrUnavailable = errors.New("events: transport unavailable")

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

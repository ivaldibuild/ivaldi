package events

import "context"

type causeKey struct{}

// cause is what events raised while handling an event inherit from it.
type cause struct {
	id            string
	correlationID string
}

// WithCause returns ctx marking e as the cause of the events raised under it,
// so Wrap links them to e's chain. Dispatch calls it for its handlers.
func WithCause(ctx context.Context, e Envelope) context.Context {
	return context.WithValue(ctx, causeKey{}, cause{id: e.ID, correlationID: e.CorrelationID})
}

func causeOf(ctx context.Context) (cause, bool) {
	c, ok := ctx.Value(causeKey{}).(cause)
	return c, ok
}

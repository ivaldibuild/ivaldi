package events

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
)

// Handler reacts to events. An event may be delivered more than once and out
// of order, so handling an event already handled must change nothing.
type Handler interface {
	Handle(ctx context.Context, e Envelope) error
}

// HandlerFunc adapts a function to a Handler.
type HandlerFunc func(ctx context.Context, e Envelope) error

func (f HandlerFunc) Handle(ctx context.Context, e Envelope) error {
	return f(ctx, e)
}

// Router sends each event to the handlers subscribed to its type. Its zero
// value is ready to use, and it is safe for concurrent use.
type Router struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// Subscribe sends events of the given types to h. A handler subscribed to
// several types tells them apart by Envelope.Type. It panics if no type is
// given, which is a programming error.
func (r *Router) Subscribe(h Handler, types ...string) {
	if len(types) == 0 {
		panic("events: a handler must subscribe to at least one event type")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = map[string][]Handler{}
	}
	for _, t := range types {
		r.handlers[t] = append(r.handlers[t], h)
	}
}

// On subscribes fn to events of type T, with their payload decoded. A saga
// that reacts to several events calls On once for each.
func On[T aggregate.Event](r *Router, fn func(ctx context.Context, e Envelope, event T) error) {
	var zero T
	r.Subscribe(HandlerFunc(func(ctx context.Context, e Envelope) error {
		event, err := Decode[T](e)
		if err != nil {
			return err
		}
		return fn(ctx, e, event)
	}), zero.EventName())
}

// Dispatch sends e to every handler subscribed to its type, in the order they
// subscribed, with e as the cause of the events they raise. It runs every
// handler even if one fails, and returns their errors joined; the caller then
// redelivers e to all of them. An event no handler subscribes to is ignored.
func (r *Router) Dispatch(ctx context.Context, e Envelope) error {
	r.mu.RLock()
	handlers := r.handlers[e.Type]
	r.mu.RUnlock()

	ctx = WithCause(ctx, e)
	var errs []error
	for _, h := range handlers {
		if err := h.Handle(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("handle event %s (%s): %w", e.ID, e.Type, err)
	}
	return nil
}

// Handle makes a Router a Handler, so a relay can dispatch to it directly. It
// is Dispatch.
func (r *Router) Handle(ctx context.Context, e Envelope) error {
	return r.Dispatch(ctx, e)
}

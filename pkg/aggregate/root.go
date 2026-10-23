// Package aggregate gives domain objects their identity and lets them record
// the events their changes raise.
//
// An aggregate embeds Root, which generates its ID when it is new (see
// NewEntity), and records an Event from the method that makes
// the change. Its service pulls the events after saving the object and saves
// them in the same transaction, so an event exists only if its change
// committed. Handlers then react to them through pkg/events.
//
// Events are notifications, not the source of truth: repositories store and
// rebuild objects from their state, never by replaying events. Constructors
// that also rebuild stored objects (NewX(props, &id)) must record a creation
// event only for a new object, or every load would raise it again.
package aggregate

// Root is an aggregate's identity and the events it raises until they are
// saved. An aggregate embeds it, set from NewRoot in its constructor. Like
// the objects that embed it, it is not safe for concurrent use.
type Root struct {
	Entity
	Events
}

// Events records the events a domain object raises until they are saved.
// Root embeds it; an aggregate identified by something other than an ID of
// its own embeds it directly. Its zero value is ready to use.
type Events struct {
	events []Event
}

// NewRoot returns the Root of an aggregate whose IDs carry prefix, with
// storedID or a new ID as NewEntity gives them.
func NewRoot(prefix string, storedID *string) (Root, error) {
	e, err := NewEntity(prefix, storedID)
	if err != nil {
		return Root{}, err
	}
	return Root{Entity: e}, nil
}

// RecordEvent records e. Domain objects call it from the methods that make
// the change e describes, and from nowhere else.
func (r *Events) RecordEvent(e Event) {
	r.events = append(r.events, e)
}

// PullEvents returns the events recorded since the last pull, in the order
// they were recorded, and forgets them so they are saved only once.
func (r *Events) PullEvents() []Event {
	events := r.events
	r.events = nil
	return events
}

package aggregate

// Event is a fact a domain object records when its state changes, such as an
// Application being archived. Events are value types: a struct with exported
// fields, which travel as JSON, and a value-receiver EventName. An event
// carries what consumers need to react to the fact, not the object's whole
// state; the audit record keeps the state before and after.
type Event interface {
	// EventName is the event's stable name, including its contract version:
	// <capability>.v<version>.<aggregate>.<event> for an aggregate's events,
	// such as "delivery.v1.application.archived", or
	// <capability>.v<version>.<event> for events no aggregate raises.
	// Handlers subscribe by it, so a change to an event's fields that
	// consumers would misread needs a new version rather than an edit.
	EventName() string
}

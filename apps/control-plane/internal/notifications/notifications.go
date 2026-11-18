// Package notifications is the control plane's notifications capability:
// it tells people about work that waits for them (docs/design/notifications.md).
//
// It reacts to domain events from the outbox stream, and owns nothing but
// the messages it sends: a Notification is one message to one person,
// pointing at what it is about, which stays the source of truth. So far it
// tells the people who could decide an approval request that one waits, and
// the person who asked how it was decided.
//
// This package holds the domain type and the interfaces. The routing
// sub-package handles the events, inbox serves the inbox over HTTP, and
// postgres stores notifications. Notifications are contact records, not
// changes anyone authorizes, so they record no domain event and no audit
// record.
package notifications

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func init() {
	id.MustRegister(Notification{}.Prefix(), "notifications.Notification")
}

// newRoot returns an aggregate's identity: storedID, a stored row's ID given
// to a New constructor, which must have the type's prefix, or a new ID.
func newRoot(prefix string, storedID *string) (aggregate.Root, error) {
	r, err := aggregate.NewRoot(prefix, storedID)
	if err != nil {
		return aggregate.Root{}, NewErrInvalidInput(err.Error())
	}
	return r, nil
}

func errorf(format string, args ...any) error {
	return NewErrInvalidInput(fmt.Sprintf(format, args...))
}

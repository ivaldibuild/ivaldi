// Package audit is the control plane's audit and provenance capability.
//
// So far it holds only the contract other capabilities depend on to record
// what they change. Each capability names its own event types.
package audit

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Event is one audit record.
type Event struct {
	// Type names what happened, such as "workspace.created". The capability
	// that records the event defines its types.
	Type           string
	OrganizationID string
	// ResourceID is the entity the event is about, or the Organization's ID
	// for an Organization-level change.
	ResourceID string
	Actor      identity.Actor
	// DecisionID is the authorizer's decision that allowed the change, if any.
	DecisionID string
	// ApprovedByPrincipalID is the human who approved the change, if any.
	ApprovedByPrincipalID string
	// Before and After are snapshots of the entity's state, such as its Data().
	Before     any
	After      any
	OccurredAt time.Time
}

// Recorder writes audit records. Calls run in the caller's transaction, so a
// change never commits without its record.
type Recorder interface {
	Record(ctx context.Context, e Event) error
}

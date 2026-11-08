// Package provenance records how Ivaldi's work becomes code: the Changes
// that tie commits in a Source to the Session and Work Item that produced
// them. Delivery derives release notes through it (ADR 0006).
//
// The domain types live here; the PostgreSQL repository is in postgres, and
// the service and HTTP API are in changes.
package provenance

import "github.com/ivaldibuild/ivaldi/pkg/id"

func init() {
	id.MustRegister(Change{}.Prefix(), "provenance.Change")
}

// Aggregate types name the domain objects that raise events.
const (
	AggregateChange = "provenance.Change"
)

// Permissions this capability asks the authorizer about. A Change belongs to
// a Source, which is an IAM resource under its Project.
const (
	PermissionChangeRead   = "change.read"
	PermissionChangeRecord = "change.record"
)

// Actions name what an actor asks the authorizer to allow.
const (
	ActionChangeList   = "change.list"
	ActionChangeRead   = "change.read"
	ActionChangeRecord = "change.record"
)

// Audit event types this capability emits.
const (
	EventChangeRecorded = "change.recorded"
)

// Limits on what one Change may hold. Text lengths match the columns.
const (
	maxTitleLength       = 512
	maxExternalRefLength = 2048
	maxCommitSHALength   = 64
	maxChangeCommits     = 100
	maxReferenceLength   = 30
)

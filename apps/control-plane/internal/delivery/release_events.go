package delivery

// Events a Release records. Each carries the Release, with its Builds, as it
// is after the change.

// ReleaseCreatedEvent is recorded when a Release is created as a draft. It
// is named apart from ReleaseCreated, the source.
type ReleaseCreatedEvent struct {
	Release ReleaseSnapshot `json:"release"`
}

// ReleasePublished is recorded when a draft is published.
type ReleasePublished struct {
	Release ReleaseSnapshot `json:"release"`
}

// ReleaseWithdrawn is recorded when a published Release is withdrawn.
type ReleaseWithdrawn struct {
	Release ReleaseSnapshot `json:"release"`
}

func (ReleaseCreatedEvent) EventName() string { return "delivery.v1.release.created" }
func (ReleasePublished) EventName() string    { return "delivery.v1.release.published" }
func (ReleaseWithdrawn) EventName() string    { return "delivery.v1.release.withdrawn" }

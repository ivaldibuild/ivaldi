package delivery

// Events a Build records. Each carries the Build as it is after the change.

// BuildRecorded is recorded when a Build is first recorded, from a CI
// report or from what the runtime agent saw running.
type BuildRecorded struct {
	Build BuildSnapshot `json:"build"`
}

// BuildReported is recorded when CI reports a Build the agent saw first,
// filling in its commit, ref, and CI details.
type BuildReported struct {
	Build BuildSnapshot `json:"build"`
}

func (BuildRecorded) EventName() string { return "delivery.v1.build.recorded" }
func (BuildReported) EventName() string { return "delivery.v1.build.reported" }

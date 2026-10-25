package iam

// Events a Workload records. Each carries the Workload as it is after the
// change. Check-ins record no event: they are contact, not changes to the
// Workload. Changes common to every principal are recorded as principal
// events.

// WorkloadCreated is recorded when a data-plane component enrolls as a
// Workload.
type WorkloadCreated struct {
	Workload WorkloadSnapshot `json:"workload"`
}

func (WorkloadCreated) EventName() string { return "iam.v1.workload.created" }

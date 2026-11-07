package iam

import "time"

// Events a WorkloadCredential records. Each carries the WorkloadCredential
// as it is after the change.

// WorkloadCredentialCreated is recorded when a Workload gets a credential.
type WorkloadCredentialCreated struct {
	WorkloadCredential WorkloadCredentialSnapshot `json:"workload_credential"`
}

// WorkloadCredentialRevoked is recorded when a WorkloadCredential is
// revoked.
type WorkloadCredentialRevoked struct {
	WorkloadCredential WorkloadCredentialSnapshot `json:"workload_credential"`
}

func (WorkloadCredentialCreated) EventName() string { return "iam.v1.workload_credential.created" }
func (WorkloadCredentialRevoked) EventName() string { return "iam.v1.workload_credential.revoked" }

// WorkloadCredentialExtended is recorded when a credential's expiry is
// moved later without replacing it.
type WorkloadCredentialExtended struct {
	WorkloadCredential WorkloadCredentialSnapshot `json:"workload_credential"`
}

// WorkloadCredentialReplaced is recorded when a new credential succeeds an
// old one and gives it a grace period.
type WorkloadCredentialReplaced struct {
	WorkloadCredential WorkloadCredentialSnapshot `json:"workload_credential"`
	PreviousNotAfter   *time.Time                 `json:"previous_not_after"`
}

func (WorkloadCredentialExtended) EventName() string {
	return "iam.v1.workload_credential.extended"
}

func (WorkloadCredentialReplaced) EventName() string {
	return "iam.v1.workload_credential.replaced"
}

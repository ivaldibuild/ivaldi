package iam

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

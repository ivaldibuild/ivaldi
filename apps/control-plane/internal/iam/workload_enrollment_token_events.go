package iam

// Events a WorkloadEnrollmentToken records. Each carries the
// WorkloadEnrollmentToken as it is after the change, without its hash.

// WorkloadEnrollmentTokenCreated is recorded when an enrollment token is
// issued.
type WorkloadEnrollmentTokenCreated struct {
	WorkloadEnrollmentToken WorkloadEnrollmentTokenSnapshot `json:"workload_enrollment_token"`
}

// WorkloadEnrollmentTokenRedeemed is recorded when a data-plane component
// enrolls with the token.
type WorkloadEnrollmentTokenRedeemed struct {
	WorkloadEnrollmentToken WorkloadEnrollmentTokenSnapshot `json:"workload_enrollment_token"`
}

func (WorkloadEnrollmentTokenCreated) EventName() string {
	return "iam.v1.workload_enrollment_token.created"
}
func (WorkloadEnrollmentTokenRedeemed) EventName() string {
	return "iam.v1.workload_enrollment_token.redeemed"
}

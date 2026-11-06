package iam

// Events every kind of principal records, through the methods User,
// ServiceAccount, AgentSession, Workload, and Agent share. They carry the state
// every principal has, including its kind; the audit record keeps the state
// before the change. Each kind's creation and own changes have their own
// events.

// PrincipalRenamed is recorded when a principal's display name changes.
type PrincipalRenamed struct {
	Principal PrincipalSnapshot `json:"principal"`
}

// PrincipalMetadataChanged is recorded when a principal's metadata changes.
type PrincipalMetadataChanged struct {
	Principal PrincipalSnapshot `json:"principal"`
}

// PrincipalStatusChanged is recorded when a principal is suspended,
// deactivated, or reactivated.
type PrincipalStatusChanged struct {
	Principal      PrincipalSnapshot `json:"principal"`
	PreviousStatus PrincipalStatus   `json:"previous_status"`
}

func (PrincipalRenamed) EventName() string         { return "iam.v1.principal.renamed" }
func (PrincipalMetadataChanged) EventName() string { return "iam.v1.principal.metadata_changed" }
func (PrincipalStatusChanged) EventName() string   { return "iam.v1.principal.status_changed" }

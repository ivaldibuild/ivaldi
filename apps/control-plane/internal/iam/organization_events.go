package iam

// Events an Organization records. Each carries the Organization as it is
// after the change; the audit record keeps the state before it.

// OrganizationCreated is recorded when an Organization is created.
type OrganizationCreated struct {
	Organization OrganizationSnapshot `json:"organization"`
}

// OrganizationUpdated is recorded when an Organization's slug, names, logo,
// or metadata change.
type OrganizationUpdated struct {
	Organization OrganizationSnapshot `json:"organization"`
}

// OrganizationStatusChanged is recorded when an Organization is suspended,
// reactivated, or deleted.
type OrganizationStatusChanged struct {
	Organization   OrganizationSnapshot `json:"organization"`
	PreviousStatus OrganizationStatus   `json:"previous_status"`
}

func (OrganizationCreated) EventName() string       { return "iam.v1.organization.created" }
func (OrganizationUpdated) EventName() string       { return "iam.v1.organization.updated" }
func (OrganizationStatusChanged) EventName() string { return "iam.v1.organization.status_changed" }

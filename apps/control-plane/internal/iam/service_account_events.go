package iam

// Events a ServiceAccount records. Each carries the ServiceAccount as it is
// after the change; the audit record keeps the state before it. Changes
// common to every principal are recorded as principal events.

// ServiceAccountCreated is recorded when a ServiceAccount is created.
type ServiceAccountCreated struct {
	ServiceAccount ServiceAccountSnapshot `json:"service_account"`
}

// ServiceAccountDescriptionChanged is recorded when a ServiceAccount's
// description changes.
type ServiceAccountDescriptionChanged struct {
	ServiceAccount ServiceAccountSnapshot `json:"service_account"`
}

func (ServiceAccountCreated) EventName() string { return "iam.v1.service_account.created" }
func (ServiceAccountDescriptionChanged) EventName() string {
	return "iam.v1.service_account.description_changed"
}

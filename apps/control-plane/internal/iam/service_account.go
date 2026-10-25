package iam

import (
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// ServiceAccount is a non-human integration in an Organization, such as CI
// or SCIM provisioning. It authenticates with API tokens.
type ServiceAccount struct {
	principal
	description          string
	createdByPrincipalID string
}

// ServiceAccountSnapshot is a ServiceAccount's state, for repositories,
// audit records, and events. Its JSON form is part of the ServiceAccount
// events' contract.
type ServiceAccountSnapshot struct {
	PrincipalSnapshot
	Description          string `json:"description"`
	CreatedByPrincipalID string `json:"created_by_principal_id"`
}

// NewServiceAccountProps describes a ServiceAccount.
type NewServiceAccountProps struct {
	PrincipalProps
	Description          string
	CreatedByPrincipalID string
}

// NewServiceAccount returns a ServiceAccount, with a new ID unless
// principalID gives a stored one.
func NewServiceAccount(props NewServiceAccountProps, principalID *string) (*ServiceAccount, error) {
	p, err := newPrincipal(PrincipalServiceAccount, props.PrincipalProps, principalID)
	if err != nil {
		return nil, err
	}
	if err := validateID("creator", principalPrefix, props.CreatedByPrincipalID); err != nil {
		return nil, err
	}
	s := &ServiceAccount{
		principal:            p,
		description:          props.Description,
		createdByPrincipalID: props.CreatedByPrincipalID,
	}
	if s.IsNew() {
		s.RecordEvent(ServiceAccountCreated{s.Snapshot()})
	}
	return s, nil
}

func (s *ServiceAccount) Description() string          { return s.description }
func (s *ServiceAccount) CreatedByPrincipalID() string { return s.createdByPrincipalID }

func (s *ServiceAccount) Snapshot() ServiceAccountSnapshot {
	return ServiceAccountSnapshot{
		PrincipalSnapshot:    s.snapshot(),
		Description:          s.description,
		CreatedByPrincipalID: s.createdByPrincipalID,
	}
}

// SetDescription replaces the description; "" removes it.
func (s *ServiceAccount) SetDescription(description string) {
	changed := description != s.description
	s.description = description
	s.updatedAt = clock.Now()
	if changed {
		s.RecordEvent(ServiceAccountDescriptionChanged{s.Snapshot()})
	}
}

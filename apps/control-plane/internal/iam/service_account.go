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

// ServiceAccountSnapshot is a ServiceAccount's state, for repositories and
// audit records.
type ServiceAccountSnapshot struct {
	PrincipalSnapshot
	Description          string
	CreatedByPrincipalID string
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
	return &ServiceAccount{
		principal:            p,
		description:          props.Description,
		createdByPrincipalID: props.CreatedByPrincipalID,
	}, nil
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
	s.description = description
	s.updatedAt = clock.Now()
}

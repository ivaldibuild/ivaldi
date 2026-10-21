package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// ExternalIdentity links a User to the subject an identity provider asserts
// for them, such as an OIDC sub or a SAML NameID. It links to the User, not
// the Account, so an Organization's identity provider can only sign people
// in to that Organization. Each subject links to one User per provider.
type ExternalIdentity struct {
	id                 string
	organizationID     string
	principalID        string
	identityProviderID string
	subject            string
	lastLoginAt        *time.Time
	createdAt          time.Time
}

// ExternalIdentitySnapshot is an ExternalIdentity's state, for repositories
// and audit records.
type ExternalIdentitySnapshot struct {
	ID                 string
	OrganizationID     string
	PrincipalID        string
	IdentityProviderID string
	Subject            string
	LastLoginAt        *time.Time
	CreatedAt          time.Time
}

// NewExternalIdentityProps describes an ExternalIdentity. A new one leaves
// the timestamps nil; a repository loading a stored one sets them.
type NewExternalIdentityProps struct {
	OrganizationID string
	// PrincipalID is the User's.
	PrincipalID        string
	IdentityProviderID string
	Subject            string
	LastLoginAt        *time.Time
	CreatedAt          *time.Time
}

// NewExternalIdentity returns an ExternalIdentity, with a new ID unless
// externalIdentityID gives a stored one.
func NewExternalIdentity(props NewExternalIdentityProps, externalIdentityID *string) (*ExternalIdentity, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateOwnID(ExternalIdentity{}.Prefix(), externalIdentityID); err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	if err := validateID("identity provider ID", IdentityProvider{}.Prefix(), props.IdentityProviderID); err != nil {
		return nil, err
	}
	if props.Subject == "" || len(props.Subject) > maxSubjectLength {
		return nil, NewErrInvalidInput(fmt.Sprintf("subject must be 1-%d bytes", maxSubjectLength))
	}

	e := &ExternalIdentity{
		organizationID:     props.OrganizationID,
		principalID:        props.PrincipalID,
		identityProviderID: props.IdentityProviderID,
		subject:            props.Subject,
		lastLoginAt:        clock.CopyTime(props.LastLoginAt),
		createdAt:          timeOr(props.CreatedAt, clock.Now()),
	}
	if externalIdentityID != nil {
		e.id = *externalIdentityID
	}
	id.Assign(&e.id, e)
	return e, nil
}

func (ExternalIdentity) Prefix() string                { return "xid" }
func (e *ExternalIdentity) ID() string                 { return e.id }
func (e *ExternalIdentity) OrganizationID() string     { return e.organizationID }
func (e *ExternalIdentity) PrincipalID() string        { return e.principalID }
func (e *ExternalIdentity) IdentityProviderID() string { return e.identityProviderID }
func (e *ExternalIdentity) Subject() string            { return e.subject }
func (e *ExternalIdentity) LastLoginAt() *time.Time    { return clock.CopyTime(e.lastLoginAt) }
func (e *ExternalIdentity) CreatedAt() time.Time       { return e.createdAt }

func (e *ExternalIdentity) Snapshot() ExternalIdentitySnapshot {
	return ExternalIdentitySnapshot{
		ID:                 e.id,
		OrganizationID:     e.organizationID,
		PrincipalID:        e.principalID,
		IdentityProviderID: e.identityProviderID,
		Subject:            e.subject,
		LastLoginAt:        clock.CopyTime(e.lastLoginAt),
		CreatedAt:          e.createdAt,
	}
}

// RecordLogin records that the identity signed in now.
func (e *ExternalIdentity) RecordLogin() {
	now := clock.Now()
	e.lastLoginAt = &now
}

package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// principalPrefix is shared by every kind of principal, whose subtype rows
// use the ID of their principals row.
const principalPrefix = "prn"

// PrincipalKind selects which subtype holds a principal's details.
type PrincipalKind string

const (
	PrincipalUser           PrincipalKind = "user"
	PrincipalServiceAccount PrincipalKind = "service_account"
	PrincipalAgentSession   PrincipalKind = "agent_session"
	PrincipalWorkload       PrincipalKind = "workload"
)

// PrincipalStatus is where a principal is in its life. Principals are never
// deleted, because grants, audit, and provenance records refer to them.
type PrincipalStatus string

const (
	PrincipalActive      PrincipalStatus = "active"
	PrincipalSuspended   PrincipalStatus = "suspended"
	PrincipalDeactivated PrincipalStatus = "deactivated"
)

// principal is what every actor in an Organization has in common: the row
// in principals that grants, group memberships, and audit records point to.
// User, ServiceAccount, AgentSession, and Workload embed it.
type principal struct {
	aggregate.Root
	organizationID string
	kind           PrincipalKind
	displayName    string
	status         PrincipalStatus
	metadata       map[string]string
	createdAt      time.Time
	updatedAt      time.Time
}

// PrincipalSnapshot is the state every principal has, for repositories and
// audit records. Each kind's snapshot embeds it.
type PrincipalSnapshot struct {
	ID             string
	OrganizationID string
	Kind           PrincipalKind
	DisplayName    string
	Status         PrincipalStatus
	Metadata       map[string]string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PrincipalProps describes what every principal has. A new one leaves
// Status and the timestamps empty; a repository loading a stored one sets them.
type PrincipalProps struct {
	OrganizationID string
	DisplayName    string
	// Status defaults to PrincipalActive.
	Status    PrincipalStatus
	Metadata  map[string]string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

func newPrincipal(kind PrincipalKind, props PrincipalProps, principalID *string) (principal, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return principal{}, err
	}
	root, err := newRoot(principalPrefix, principalID)
	if err != nil {
		return principal{}, err
	}
	if err := validateName("display name", props.DisplayName, maxNameLength); err != nil {
		return principal{}, err
	}
	if props.Status == "" {
		props.Status = PrincipalActive
	}
	if err := validatePrincipalStatus(props.Status); err != nil {
		return principal{}, err
	}
	if err := validateMetadata(props.Metadata); err != nil {
		return principal{}, err
	}

	now := clock.Now()
	p := principal{
		Root:           root,
		organizationID: props.OrganizationID,
		kind:           kind,
		displayName:    props.DisplayName,
		status:         props.Status,
		metadata:       copyMetadata(props.Metadata),
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	return p, nil
}

func validatePrincipalStatus(s PrincipalStatus) error {
	switch s {
	case PrincipalActive, PrincipalSuspended, PrincipalDeactivated:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("principal status %q must be active, suspended, or deactivated", s))
}

func (principal) Prefix() string                 { return principalPrefix }
func (p *principal) OrganizationID() string      { return p.organizationID }
func (p *principal) Kind() PrincipalKind         { return p.kind }
func (p *principal) DisplayName() string         { return p.displayName }
func (p *principal) Status() PrincipalStatus     { return p.status }
func (p *principal) Metadata() map[string]string { return copyMetadata(p.metadata) }
func (p *principal) CreatedAt() time.Time        { return p.createdAt }
func (p *principal) UpdatedAt() time.Time        { return p.updatedAt }

func (p *principal) Active() bool { return p.status == PrincipalActive }

func (p *principal) snapshot() PrincipalSnapshot {
	return PrincipalSnapshot{
		ID:             p.ID(),
		OrganizationID: p.organizationID,
		Kind:           p.kind,
		DisplayName:    p.displayName,
		Status:         p.status,
		Metadata:       copyMetadata(p.metadata),
		CreatedAt:      p.createdAt,
		UpdatedAt:      p.updatedAt,
	}
}

// Rename changes the principal's display name.
func (p *principal) Rename(displayName string) error {
	if err := validateName("display name", displayName, maxNameLength); err != nil {
		return err
	}
	p.displayName = displayName
	p.updatedAt = clock.Now()
	return nil
}

// SetMetadata replaces the principal's metadata; nil removes it.
func (p *principal) SetMetadata(metadata map[string]string) error {
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	p.metadata = copyMetadata(metadata)
	p.updatedAt = clock.Now()
	return nil
}

// Suspend stops an active principal from acting until it is reactivated.
// Suspending a suspended one does nothing.
func (p *principal) Suspend() error {
	return p.setStatus(PrincipalSuspended, PrincipalActive)
}

// Deactivate stops the principal from acting, such as when a member leaves
// the Organization. Deactivating a deactivated one does nothing.
func (p *principal) Deactivate() error {
	return p.setStatus(PrincipalDeactivated, PrincipalActive, PrincipalSuspended)
}

// Reactivate makes a suspended or deactivated principal active again.
func (p *principal) Reactivate() error {
	return p.setStatus(PrincipalActive, PrincipalSuspended, PrincipalDeactivated)
}

// setStatus moves the principal to status from one of the from statuses.
func (p *principal) setStatus(status PrincipalStatus, from ...PrincipalStatus) error {
	if p.status == status {
		return nil
	}
	for _, f := range from {
		if p.status == f {
			p.status = status
			p.updatedAt = clock.Now()
			return nil
		}
	}
	return fmt.Errorf("%w: principal %s is %s", ErrStatus, p.ID(), p.status)
}

package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// WorkloadCredentialKind is how an enrolled Workload proves who it is.
type WorkloadCredentialKind string

const (
	// CredentialMTLSCertificate identifies the Workload by a client
	// certificate; the identifier is the certificate's fingerprint.
	CredentialMTLSCertificate WorkloadCredentialKind = "mtls_certificate"
	// CredentialKubernetesServiceAccount identifies the Workload by a
	// Kubernetes service account; the identifier names the cluster,
	// namespace, and service account.
	CredentialKubernetesServiceAccount WorkloadCredentialKind = "kubernetes_service_account"
)

// WorkloadCredential is one way an enrolled Workload authenticates. Each
// (kind, identifier) belongs to one Workload.
type WorkloadCredential struct {
	aggregate.Root
	organizationID string
	principalID    string
	kind           WorkloadCredentialKind
	identifier     string
	notAfter       *time.Time
	revokedAt      *time.Time
	createdAt      time.Time
}

// WorkloadCredentialSnapshot is a WorkloadCredential's state, for
// repositories and audit records.
type WorkloadCredentialSnapshot struct {
	ID             string
	OrganizationID string
	PrincipalID    string
	Kind           WorkloadCredentialKind
	Identifier     string
	NotAfter       *time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// NewWorkloadCredentialProps describes a WorkloadCredential. A new one
// leaves RevokedAt and CreatedAt nil; a repository loading a stored one sets them.
type NewWorkloadCredentialProps struct {
	OrganizationID string
	// PrincipalID is the Workload's.
	PrincipalID string
	Kind        WorkloadCredentialKind
	Identifier  string
	// NotAfter is when the credential stops being accepted, such as a
	// certificate's expiry; nil means it does not expire.
	NotAfter  *time.Time
	RevokedAt *time.Time
	CreatedAt *time.Time
}

// NewWorkloadCredential returns a WorkloadCredential, with a new ID unless
// credentialID gives a stored one.
func NewWorkloadCredential(props NewWorkloadCredentialProps, credentialID *string) (*WorkloadCredential, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(WorkloadCredential{}.Prefix(), credentialID)
	if err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	switch props.Kind {
	case CredentialMTLSCertificate, CredentialKubernetesServiceAccount:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("workload credential kind %q must be mtls_certificate or kubernetes_service_account", props.Kind))
	}
	if err := validateRequired("identifier", props.Identifier); err != nil {
		return nil, err
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateOptionalExpiry(props.NotAfter, createdAt); err != nil {
		return nil, err
	}

	c := &WorkloadCredential{
		Root:           root,
		organizationID: props.OrganizationID,
		principalID:    props.PrincipalID,
		kind:           props.Kind,
		identifier:     props.Identifier,
		notAfter:       clock.CopyTime(props.NotAfter),
		revokedAt:      clock.CopyTime(props.RevokedAt),
		createdAt:      createdAt,
	}
	return c, nil
}

func (WorkloadCredential) Prefix() string                  { return "wcr" }
func (c *WorkloadCredential) OrganizationID() string       { return c.organizationID }
func (c *WorkloadCredential) PrincipalID() string          { return c.principalID }
func (c *WorkloadCredential) Kind() WorkloadCredentialKind { return c.kind }
func (c *WorkloadCredential) Identifier() string           { return c.identifier }
func (c *WorkloadCredential) NotAfter() *time.Time         { return clock.CopyTime(c.notAfter) }
func (c *WorkloadCredential) RevokedAt() *time.Time        { return clock.CopyTime(c.revokedAt) }
func (c *WorkloadCredential) CreatedAt() time.Time         { return c.createdAt }

func (c *WorkloadCredential) Revoked() bool { return c.revokedAt != nil }
func (c *WorkloadCredential) Expired() bool {
	return c.notAfter != nil && !clock.Now().Before(*c.notAfter)
}

// CheckUsable returns an error if the credential is revoked or expired. Its
// Workload must be checked too.
func (c *WorkloadCredential) CheckUsable() error {
	switch {
	case c.Revoked():
		return fmt.Errorf("%w: workload credential %s", ErrRevoked, c.ID())
	case c.Expired():
		return fmt.Errorf("%w: workload credential %s", ErrExpired, c.ID())
	}
	return nil
}

func (c *WorkloadCredential) Snapshot() WorkloadCredentialSnapshot {
	return WorkloadCredentialSnapshot{
		ID:             c.ID(),
		OrganizationID: c.organizationID,
		PrincipalID:    c.principalID,
		Kind:           c.kind,
		Identifier:     c.identifier,
		NotAfter:       clock.CopyTime(c.notAfter),
		RevokedAt:      clock.CopyTime(c.revokedAt),
		CreatedAt:      c.createdAt,
	}
}

// Revoke stops the credential from being accepted. Revoking a revoked one
// does nothing.
func (c *WorkloadCredential) Revoke() {
	if !c.Revoked() {
		now := clock.Now()
		c.revokedAt = &now
	}
}

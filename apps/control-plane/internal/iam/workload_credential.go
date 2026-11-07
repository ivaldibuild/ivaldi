package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
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
	// CredentialBearerToken identifies the Workload by an opaque bearer
	// token. Only its hash is stored.
	CredentialBearerToken WorkloadCredentialKind = "bearer_token"
)

// WorkloadCredential is one way an enrolled Workload authenticates. Each
// (kind, identifier) belongs to one Workload.
type WorkloadCredential struct {
	aggregate.Root
	organizationID string
	principalID    string
	kind           WorkloadCredentialKind
	identifier     string
	secretHash     []byte
	notAfter       *time.Time
	revokedAt      *time.Time
	replacedBy     *string
	createdAt      time.Time
}

// WorkloadCredentialSnapshot is a WorkloadCredential's state, for
// repositories, audit records, and events. Its JSON form is part of the
// WorkloadCredential events' contract.
type WorkloadCredentialSnapshot struct {
	ID             string                 `json:"id"`
	OrganizationID string                 `json:"organization_id"`
	PrincipalID    string                 `json:"principal_id"`
	Kind           WorkloadCredentialKind `json:"kind"`
	Identifier     string                 `json:"identifier"`
	SecretHash     []byte                 `json:"-"`
	NotAfter       *time.Time             `json:"not_after"`
	RevokedAt      *time.Time             `json:"revoked_at"`
	ReplacedBy     *string                `json:"replaced_by"`
	CreatedAt      time.Time              `json:"created_at"`
}

// NewWorkloadCredentialProps describes a WorkloadCredential. A new one
// leaves RevokedAt and CreatedAt nil; a repository loading a stored one sets them.
type NewWorkloadCredentialProps struct {
	OrganizationID string
	// PrincipalID is the Workload's.
	PrincipalID string
	Kind        WorkloadCredentialKind
	Identifier  string
	// SecretHash is the SHA-256 hash of a bearer token, or nil for other kinds.
	SecretHash []byte
	// NotAfter is when the credential stops being accepted, such as a
	// certificate's expiry; nil means it does not expire.
	NotAfter  *time.Time
	RevokedAt *time.Time
	// ReplacedBy names the credential that succeeded this one.
	ReplacedBy *string
	CreatedAt  *time.Time
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
		if len(props.SecretHash) != 0 {
			return nil, NewErrInvalidInput("only a bearer token credential has a secret hash")
		}
	case CredentialBearerToken:
		if err := validateTokenHash("secret hash", props.SecretHash); err != nil {
			return nil, err
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("workload credential kind %q must be mtls_certificate, kubernetes_service_account, or bearer_token", props.Kind))
	}
	if err := validateRequired("identifier", props.Identifier); err != nil {
		return nil, err
	}
	if props.ReplacedBy != nil && !id.Valid(WorkloadCredential{}.Prefix(), *props.ReplacedBy) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a workload credential ID", *props.ReplacedBy))
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
		secretHash:     copyBytes(props.SecretHash),
		notAfter:       clock.CopyTime(props.NotAfter),
		revokedAt:      clock.CopyTime(props.RevokedAt),
		replacedBy:     optionalString(props.ReplacedBy),
		createdAt:      createdAt,
	}
	if c.IsNew() {
		c.RecordEvent(WorkloadCredentialCreated{c.Snapshot()})
	}
	return c, nil
}

func (WorkloadCredential) Prefix() string                  { return "wcr" }
func (c *WorkloadCredential) OrganizationID() string       { return c.organizationID }
func (c *WorkloadCredential) PrincipalID() string          { return c.principalID }
func (c *WorkloadCredential) Kind() WorkloadCredentialKind { return c.kind }
func (c *WorkloadCredential) Identifier() string           { return c.identifier }
func (c *WorkloadCredential) SecretHash() []byte           { return copyBytes(c.secretHash) }
func (c *WorkloadCredential) NotAfter() *time.Time         { return clock.CopyTime(c.notAfter) }
func (c *WorkloadCredential) RevokedAt() *time.Time        { return clock.CopyTime(c.revokedAt) }
func (c *WorkloadCredential) ReplacedBy() *string          { return copyString(c.replacedBy) }
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
		SecretHash:     copyBytes(c.secretHash),
		NotAfter:       clock.CopyTime(c.notAfter),
		RevokedAt:      clock.CopyTime(c.revokedAt),
		ReplacedBy:     copyString(c.replacedBy),
		CreatedAt:      c.createdAt,
	}
}

// Revoke stops the credential from being accepted. Revoking a revoked one
// does nothing.
func (c *WorkloadCredential) Revoke() {
	if !c.Revoked() {
		now := clock.Now()
		c.revokedAt = &now
		c.RecordEvent(WorkloadCredentialRevoked{c.Snapshot()})
	}
}

// Extend moves the credential's expiry later. It never shortens it.
func (c *WorkloadCredential) Extend(notAfter time.Time) error {
	if c.Revoked() {
		return fmt.Errorf("%w: workload credential %s", ErrRevoked, c.ID())
	}
	if !notAfter.After(c.createdAt) {
		return NewErrInvalidInput("a credential expiry must be after it was created")
	}
	if c.notAfter != nil && !notAfter.After(*c.notAfter) {
		return nil
	}
	c.notAfter = clock.CopyTime(&notAfter)
	c.RecordEvent(WorkloadCredentialExtended{c.Snapshot()})
	return nil
}

// Supersede records that replacedBy succeeded this credential, giving this
// one a grace period before it stops being accepted.
func (c *WorkloadCredential) Supersede(replacedBy string, graceUntil time.Time) error {
	if c.Revoked() {
		return nil
	}
	if !id.Valid(WorkloadCredential{}.Prefix(), replacedBy) {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a workload credential ID", replacedBy))
	}
	previous := clock.CopyTime(c.notAfter)
	c.replacedBy = &replacedBy
	if c.notAfter == nil || graceUntil.Before(*c.notAfter) {
		c.notAfter = clock.CopyTime(&graceUntil)
	}
	c.RecordEvent(WorkloadCredentialReplaced{WorkloadCredential: c.Snapshot(), PreviousNotAfter: previous})
	return nil
}

package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
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
	// CredentialPublicKey identifies the Workload by a key pair: it signs
	// each request, and only its public key is stored. The identifier is
	// the key's JWK thumbprint. Only platform workloads hold one so far
	// (ADR 0015).
	CredentialPublicKey WorkloadCredentialKind = "public_key"
)

// WorkloadCredential is one way an enrolled Workload authenticates. Each
// (kind, identifier) belongs to one Workload.
type WorkloadCredential struct {
	aggregate.Root
	platform       bool
	organizationID string
	principalID    string
	kind           WorkloadCredentialKind
	identifier     string
	secretHash     []byte
	publicKey      []byte
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
	Platform       bool                   `json:"platform"`
	OrganizationID string                 `json:"organization_id"`
	PrincipalID    string                 `json:"principal_id"`
	Kind           WorkloadCredentialKind `json:"kind"`
	Identifier     string                 `json:"identifier"`
	SecretHash     []byte                 `json:"-"`
	// PublicKey is a public key credential's key, DER-encoded (PKIX).
	PublicKey  []byte     `json:"-"`
	NotAfter   *time.Time `json:"not_after"`
	RevokedAt  *time.Time `json:"revoked_at"`
	ReplacedBy *string    `json:"replaced_by"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewWorkloadCredentialProps describes a WorkloadCredential. A new one
// leaves RevokedAt and CreatedAt nil; a repository loading a stored one sets them.
type NewWorkloadCredentialProps struct {
	// Platform marks a platform workload's credential, which has no
	// OrganizationID.
	Platform       bool
	OrganizationID string
	// PrincipalID is the Workload's.
	PrincipalID string
	Kind        WorkloadCredentialKind
	Identifier  string
	// SecretHash is the SHA-256 hash of a bearer token, or nil for other kinds.
	SecretHash []byte
	// PublicKey is a public key credential's key, DER-encoded (PKIX), or nil
	// for other kinds. Identifier must be its thumbprint.
	PublicKey []byte
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
	if props.Platform {
		if props.OrganizationID != "" {
			return nil, NewErrInvalidInput("a platform workload's credential belongs to no organization")
		}
	} else if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(WorkloadCredential{}.Prefix(), credentialID)
	if err != nil {
		return nil, err
	}
	if err := validateID("principal ID", principalPrefix, props.PrincipalID); err != nil {
		return nil, err
	}
	if props.Kind != CredentialBearerToken && len(props.SecretHash) != 0 {
		return nil, NewErrInvalidInput("only a bearer token credential has a secret hash")
	}
	if props.Kind != CredentialPublicKey && len(props.PublicKey) != 0 {
		return nil, NewErrInvalidInput("only a public key credential has a public key")
	}
	switch props.Kind {
	case CredentialMTLSCertificate, CredentialKubernetesServiceAccount:
	case CredentialBearerToken:
		if err := validateTokenHash("secret hash", props.SecretHash); err != nil {
			return nil, err
		}
	case CredentialPublicKey:
		if !props.Platform {
			return nil, NewErrInvalidInput("only a platform workload authenticates with a public key")
		}
		if err := validatePublicKey(props.PublicKey, props.Identifier); err != nil {
			return nil, err
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("workload credential kind %q must be mtls_certificate, kubernetes_service_account, bearer_token, or public_key", props.Kind))
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
		platform:       props.Platform,
		organizationID: props.OrganizationID,
		principalID:    props.PrincipalID,
		kind:           props.Kind,
		identifier:     props.Identifier,
		secretHash:     copyBytes(props.SecretHash),
		publicKey:      copyBytes(props.PublicKey),
		notAfter:       clock.CopyTime(props.NotAfter),
		revokedAt:      clock.CopyTime(props.RevokedAt),
		replacedBy:     optional.OfPtr(props.ReplacedBy),
		createdAt:      createdAt,
	}
	if c.IsNew() {
		c.RecordEvent(WorkloadCredentialCreated{c.Snapshot()})
	}
	return c, nil
}

func (WorkloadCredential) Prefix() string                  { return "wcr" }
func (c *WorkloadCredential) OrganizationID() string       { return c.organizationID }
func (c *WorkloadCredential) Platform() bool               { return c.platform }
func (c *WorkloadCredential) PublicKey() []byte            { return copyBytes(c.publicKey) }
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
		Platform:       c.platform,
		OrganizationID: c.organizationID,
		PrincipalID:    c.principalID,
		Kind:           c.kind,
		Identifier:     c.identifier,
		SecretHash:     copyBytes(c.secretHash),
		PublicKey:      copyBytes(c.publicKey),
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

// NewPlatformKeyCredential returns a public key credential for a platform
// workload, from its DER-encoded (PKIX) public key.
func NewPlatformKeyCredential(principalID string, publicKeyDER []byte) (*WorkloadCredential, error) {
	public, err := identity.ParseWorkloadPublicKeyDER(publicKeyDER)
	if err != nil {
		return nil, NewErrInvalidInput(err.Error())
	}
	thumbprint, err := identity.WorkloadKeyThumbprint(public)
	if err != nil {
		return nil, NewErrInvalidInput(err.Error())
	}
	return NewWorkloadCredential(NewWorkloadCredentialProps{
		Platform:    true,
		PrincipalID: principalID,
		Kind:        CredentialPublicKey,
		Identifier:  thumbprint,
		PublicKey:   publicKeyDER,
	}, nil)
}

// validatePublicKey checks a public key credential's key and that its
// identifier is the key's thumbprint, so verification finds the right key.
func validatePublicKey(der []byte, identifier string) error {
	if len(der) == 0 {
		return NewErrInvalidInput("a public key credential needs its public key")
	}
	public, err := identity.ParseWorkloadPublicKeyDER(der)
	if err != nil {
		return NewErrInvalidInput(err.Error())
	}
	thumbprint, err := identity.WorkloadKeyThumbprint(public)
	if err != nil {
		return NewErrInvalidInput(err.Error())
	}
	if identifier != thumbprint {
		return NewErrInvalidInput("a public key credential's identifier must be its key's thumbprint")
	}
	return nil
}

package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// WorkloadEnrollmentToken lets one data-plane component enroll as a
// Workload in a pool. It is single-use and short-lived, and only its
// SHA-256 hash is stored.
type WorkloadEnrollmentToken struct {
	aggregate.Root
	organizationID       string
	tokenHash            []byte
	pool                 string
	targetID             *string
	credentialKind       WorkloadCredentialKind
	createdByPrincipalID string
	expiresAt            time.Time
	usedAt               *time.Time
	usedByPrincipalID    *string
	createdAt            time.Time
}

// WorkloadEnrollmentTokenSnapshot is a WorkloadEnrollmentToken's state, for
// repositories, audit records, and events. Its JSON form is part of the
// WorkloadEnrollmentToken events' contract. Its hash is left out of JSON, so
// it never reaches an event.
type WorkloadEnrollmentTokenSnapshot struct {
	ID                   string                 `json:"id"`
	OrganizationID       string                 `json:"organization_id"`
	TokenHash            []byte                 `json:"-"`
	Pool                 string                 `json:"pool"`
	TargetID             *string                `json:"target_id"`
	CredentialKind       WorkloadCredentialKind `json:"credential_kind"`
	CreatedByPrincipalID string                 `json:"created_by_principal_id"`
	ExpiresAt            time.Time              `json:"expires_at"`
	UsedAt               *time.Time             `json:"used_at"`
	UsedByPrincipalID    *string                `json:"used_by_principal_id"`
	CreatedAt            time.Time              `json:"created_at"`
}

// NewWorkloadEnrollmentTokenProps describes a WorkloadEnrollmentToken. A
// new one leaves UsedAt, UsedByPrincipalID, and CreatedAt nil; a repository
// loading a stored one sets them.
type NewWorkloadEnrollmentTokenProps struct {
	OrganizationID string
	// TokenHash is the SHA-256 hash of the token.
	TokenHash []byte
	// Pool is the pool the enrolled Workload joins.
	Pool string
	// TargetID is the Target a runtime agent serves, when the token is for one.
	TargetID *string
	// CredentialKind is the credential the enrolled Workload will use.
	CredentialKind       WorkloadCredentialKind
	CreatedByPrincipalID string
	ExpiresAt            time.Time
	UsedAt               *time.Time
	UsedByPrincipalID    *string
	CreatedAt            *time.Time
}

// NewWorkloadEnrollmentToken returns a WorkloadEnrollmentToken, with a new
// ID unless tokenID gives a stored one.
func NewWorkloadEnrollmentToken(props NewWorkloadEnrollmentTokenProps, tokenID *string) (*WorkloadEnrollmentToken, error) {
	if err := validateID("organization ID", Organization{}.Prefix(), props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(WorkloadEnrollmentToken{}.Prefix(), tokenID)
	if err != nil {
		return nil, err
	}
	if err := validateTokenHash("token hash", props.TokenHash); err != nil {
		return nil, err
	}
	if err := validateName("pool", props.Pool, maxPoolLength); err != nil {
		return nil, err
	}
	if props.TargetID != nil && !id.Valid("tgt", *props.TargetID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a target ID", *props.TargetID))
	}
	if props.CredentialKind == "" {
		props.CredentialKind = CredentialBearerToken
	}
	switch props.CredentialKind {
	case CredentialBearerToken, CredentialMTLSCertificate, CredentialKubernetesServiceAccount:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("credential kind %q must be bearer_token, mtls_certificate, or kubernetes_service_account", props.CredentialKind))
	}
	if err := validateID("creator", principalPrefix, props.CreatedByPrincipalID); err != nil {
		return nil, err
	}
	createdAt := timeOr(props.CreatedAt, clock.Now())
	if err := validateExpiry(props.ExpiresAt, createdAt); err != nil {
		return nil, err
	}
	usedBy := optional.OfPtr(props.UsedByPrincipalID)
	if (props.UsedAt == nil) != (usedBy == nil) {
		return nil, NewErrInvalidInput("a used enrollment token needs both when and by whom it was used")
	}
	if usedBy != nil {
		if err := validateID("enrolled workload", principalPrefix, *usedBy); err != nil {
			return nil, err
		}
	}

	t := &WorkloadEnrollmentToken{
		Root:                 root,
		organizationID:       props.OrganizationID,
		tokenHash:            copyBytes(props.TokenHash),
		pool:                 props.Pool,
		targetID:             optional.OfPtr(props.TargetID),
		credentialKind:       props.CredentialKind,
		createdByPrincipalID: props.CreatedByPrincipalID,
		expiresAt:            props.ExpiresAt,
		usedAt:               clock.CopyTime(props.UsedAt),
		usedByPrincipalID:    usedBy,
		createdAt:            createdAt,
	}
	if t.IsNew() {
		t.RecordEvent(WorkloadEnrollmentTokenCreated{t.Snapshot()})
	}
	return t, nil
}

func (WorkloadEnrollmentToken) Prefix() string            { return "wet" }
func (t *WorkloadEnrollmentToken) OrganizationID() string { return t.organizationID }
func (t *WorkloadEnrollmentToken) TokenHash() []byte      { return copyBytes(t.tokenHash) }
func (t *WorkloadEnrollmentToken) Pool() string           { return t.pool }
func (t *WorkloadEnrollmentToken) TargetID() *string      { return copyString(t.targetID) }
func (t *WorkloadEnrollmentToken) CredentialKind() WorkloadCredentialKind {
	return t.credentialKind
}
func (t *WorkloadEnrollmentToken) CreatedByPrincipalID() string { return t.createdByPrincipalID }
func (t *WorkloadEnrollmentToken) ExpiresAt() time.Time         { return t.expiresAt }
func (t *WorkloadEnrollmentToken) UsedAt() *time.Time           { return clock.CopyTime(t.usedAt) }
func (t *WorkloadEnrollmentToken) UsedByPrincipalID() *string   { return copyString(t.usedByPrincipalID) }
func (t *WorkloadEnrollmentToken) CreatedAt() time.Time         { return t.createdAt }

func (t *WorkloadEnrollmentToken) Used() bool    { return t.usedAt != nil }
func (t *WorkloadEnrollmentToken) Expired() bool { return !clock.Now().Before(t.expiresAt) }

func (t *WorkloadEnrollmentToken) Snapshot() WorkloadEnrollmentTokenSnapshot {
	return WorkloadEnrollmentTokenSnapshot{
		ID:                   t.ID(),
		OrganizationID:       t.organizationID,
		TokenHash:            copyBytes(t.tokenHash),
		Pool:                 t.pool,
		TargetID:             copyString(t.targetID),
		CredentialKind:       t.credentialKind,
		CreatedByPrincipalID: t.createdByPrincipalID,
		ExpiresAt:            t.expiresAt,
		UsedAt:               clock.CopyTime(t.usedAt),
		UsedByPrincipalID:    copyString(t.usedByPrincipalID),
		CreatedAt:            t.createdAt,
	}
}

// Redeem uses the token to enroll a Workload, which must be in the token's
// Organization and pool. A token can be redeemed once, before it expires.
func (t *WorkloadEnrollmentToken) Redeem(w *Workload) error {
	switch {
	case t.Used():
		return fmt.Errorf("%w: enrollment token %s", ErrAlreadyUsed, t.ID())
	case t.Expired():
		return fmt.Errorf("%w: enrollment token %s", ErrExpired, t.ID())
	case w.OrganizationID() != t.organizationID:
		return NewErrInvalidInput(fmt.Sprintf("workload %s is not in the enrollment token's organization", w.ID()))
	case w.Pool() != t.pool:
		return NewErrInvalidInput(fmt.Sprintf("workload %s is in pool %q, not the enrollment token's pool %q", w.ID(), w.Pool(), t.pool))
	}
	now := clock.Now()
	workloadID := w.ID()
	t.usedAt = &now
	t.usedByPrincipalID = &workloadID
	t.RecordEvent(WorkloadEnrollmentTokenRedeemed{t.Snapshot()})
	return nil
}

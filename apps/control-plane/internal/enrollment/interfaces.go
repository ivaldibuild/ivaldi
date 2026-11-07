// Package enrollment creates and redeems Target enrolments for data-plane
// workloads, and renews their credentials. It joins IAM's workload identity
// with delivery's Targets without making either capability depend on the
// other.
package enrollment

import (
	"context"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CredentialMethod is how an enrolled workload authenticates.
type CredentialMethod string

const (
	MethodBearer     CredentialMethod = "bearer"
	MethodMTLS       CredentialMethod = "mtls"
	MethodKubernetes CredentialMethod = "kubernetes"
)

func (m CredentialMethod) Kind() iam.WorkloadCredentialKind {
	switch m {
	case MethodMTLS:
		return iam.CredentialMTLSCertificate
	case MethodKubernetes:
		return iam.CredentialKubernetesServiceAccount
	default:
		return iam.CredentialBearerToken
	}
}

func (m CredentialMethod) Valid() bool {
	switch m {
	case MethodBearer, MethodMTLS, MethodKubernetes:
		return true
	}
	return false
}

// CreateInput asks for a Target enrolment token.
type CreateInput struct {
	OrganizationID string
	TargetID       string
	Method         CredentialMethod
}

// IssuedEnrolment is a new single-use enrolment token.
type IssuedEnrolment struct {
	TargetID  string           `json:"target_id"`
	Method    CredentialMethod `json:"method"`
	Token     string           `json:"token"`
	Pool      string           `json:"pool"`
	ExpiresAt time.Time        `json:"expires_at"`
}

// RedeemInput exchanges an enrolment token for a workload credential.
type RedeemInput struct {
	Token                 string
	Method                CredentialMethod
	KubernetesToken       string
	ClientCertificateFP   string
	ClientCertificateEnds time.Time
}

// Redeemed is the credential a workload got at enrolment.
type Redeemed struct {
	WorkloadPrincipalID string                     `json:"workload_principal_id"`
	CredentialID        string                     `json:"credential_id"`
	CredentialKind      iam.WorkloadCredentialKind `json:"credential_kind"`
	WorkloadToken       *string                    `json:"workload_token,omitempty"`
	ExpiresAt           *time.Time                 `json:"expires_at"`
}

// Renewed is the renewed credential, with a new token only for bearer.
type Renewed struct {
	CredentialID      string                     `json:"credential_id"`
	CredentialKind    iam.WorkloadCredentialKind `json:"credential_kind"`
	WorkloadToken     *string                    `json:"workload_token,omitempty"`
	ExpiresAt         *time.Time                 `json:"expires_at"`
	PreviousExpiresAt *time.Time                 `json:"previous_expires_at"`
}

// AgentStatus describes the agent assigned to a Target.
type AgentStatus struct {
	AgentPrincipalID    *string                     `json:"agent_principal_id"`
	Component           iam.WorkloadComponent       `json:"component"`
	CredentialKind      *iam.WorkloadCredentialKind `json:"credential_kind"`
	CredentialExpiresAt *time.Time                  `json:"credential_expires_at"`
	Status              string                      `json:"status"`
	LastSeenAt          *time.Time                  `json:"last_seen_at"`
}

// Targets is the delivery capability's Target repository, narrowed to what
// enrolment needs.
type Targets interface {
	GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	GetTargetByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*delivery.Target, error)
	UpdateTarget(ctx context.Context, t *delivery.Target) error
}

// Environments is the delivery capability's Environment repository, narrowed
// to what enrolment needs.
type Environments interface {
	GetEnvironment(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error)
}

// Service manages Target enrolment and workload credential renewal.
type Service interface {
	Create(ctx context.Context, actor identity.Actor, in CreateInput) (*IssuedEnrolment, error)
	Redeem(ctx context.Context, in RedeemInput) (*Redeemed, error)
	Renew(ctx context.Context, caller identity.Caller) (*Renewed, error)
	Status(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*AgentStatus, error)
}

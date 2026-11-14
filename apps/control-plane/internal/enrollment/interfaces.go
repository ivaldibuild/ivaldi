// Package enrollment creates and redeems Cluster enrolments for data-plane
// workloads, and renews their credentials. It joins IAM's workload identity
// with delivery's Clusters without making either capability depend on the
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

// CreateInput asks for a Cluster enrolment token.
type CreateInput struct {
	OrganizationID string
	ClusterID      string
	Method         CredentialMethod
}

// IssuedEnrolment is a new single-use enrolment token.
type IssuedEnrolment struct {
	ClusterID string           `json:"cluster_id"`
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

// Redeemed is the credential a workload got at enrolment. It names the
// Cluster and Organization so the agent can keep them in its state Secret.
type Redeemed struct {
	OrganizationID      string                     `json:"organization_id"`
	ClusterID           string                     `json:"cluster_id"`
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

// AgentStatus describes the agent enrolled for a Cluster.
type AgentStatus struct {
	ClusterID           string                      `json:"cluster_id"`
	AgentPrincipalID    *string                     `json:"agent_principal_id"`
	Component           iam.WorkloadComponent       `json:"component"`
	CredentialKind      *iam.WorkloadCredentialKind `json:"credential_kind"`
	CredentialExpiresAt *time.Time                  `json:"credential_expires_at"`
	Status              string                      `json:"status"`
	LastSeenAt          *time.Time                  `json:"last_seen_at"`
	RedeemedAt          *time.Time                  `json:"redeemed_at"`
	AgentVersion        string                      `json:"agent_version,omitempty"`
	KubernetesVersion   string                      `json:"kubernetes_version,omitempty"`
	// LastFailure is the last refused enrolment for the Cluster, so the
	// person enrolling can see why without reading server logs.
	LastFailure *delivery.AgentFailure `json:"last_failure,omitempty"`
}

// Clusters is the delivery capability's Cluster repository, narrowed to what
// enrolment needs. A Cluster's agent is the only thing enrolment assigns.
type Clusters interface {
	GetCluster(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error)
	GetClusterForUpdate(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error)
	GetClusterByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*delivery.Cluster, error)
	UpdateCluster(ctx context.Context, c *delivery.Cluster) error
}

// Service manages Cluster enrolment and workload credential renewal.
type Service interface {
	Create(ctx context.Context, actor identity.Actor, in CreateInput) (*IssuedEnrolment, error)
	Redeem(ctx context.Context, in RedeemInput) (*Redeemed, error)
	Renew(ctx context.Context, caller identity.Caller) (*Renewed, error)
	Status(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*AgentStatus, error)
}

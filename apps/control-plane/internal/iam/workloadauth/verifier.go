// Package workloadauth authenticates enrolled data-plane workloads with the
// three credential kinds IAM supports: bearer tokens, client certificates,
// and Kubernetes service-account tokens. It is the workload half of the API
// verifier chain.
package workloadauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const (
	// ClientIDs identify how a workload authenticated.
	ClientBearer     = "workload_bearer"
	ClientMTLS       = "workload_mtls"
	ClientKubernetes = "workload_kubernetes"

	lastSeenInterval = time.Minute
)

// Credentials reads workload credentials. iam.WorkloadCredentialRepository
// provides it.
type Credentials interface {
	GetBySecretHash(ctx context.Context, hash []byte) (*iam.WorkloadCredential, error)
	GetByKindAndIdentifier(ctx context.Context, kind iam.WorkloadCredentialKind, identifier string) (*iam.WorkloadCredential, error)
}

// Workloads reads and records contact with workload principals.
// iam.WorkloadRepository provides it.
type Workloads interface {
	Get(ctx context.Context, organizationID, principalID string) (*iam.Workload, error)
	Write(ctx context.Context, w *iam.Workload) error
}

// NewVerifier returns a verifier for workload credentials, falling through
// to next for every credential it does not recognize.
func NewVerifier(credentials Credentials, workloads Workloads, kubernetes KubernetesVerifier, next identity.Verifier) (identity.Verifier, error) {
	switch {
	case credentials == nil:
		return nil, errors.New("workload verifier: Credentials is required")
	case workloads == nil:
		return nil, errors.New("workload verifier: Workloads is required")
	case next == nil:
		return nil, errors.New("workload verifier: an access token Verifier is required")
	}
	return &verifier{credentials: credentials, workloads: workloads, kubernetes: kubernetes, next: next}, nil
}

type verifier struct {
	credentials Credentials
	workloads   Workloads
	kubernetes  KubernetesVerifier
	next        identity.Verifier
}

func (v *verifier) Verify(ctx context.Context, credential string) (*identity.Claims, error) {
	switch {
	case strings.HasPrefix(credential, identity.MTLSCredentialPrefix):
		fingerprint, certExpiry, err := identity.ParseMTLSCredential(credential)
		if err != nil {
			return nil, err
		}
		return v.fromCredential(ctx, iam.CredentialMTLSCertificate, fingerprint, ClientMTLS, certExpiry)
	case strings.HasPrefix(credential, identity.KubernetesCredentialPrefix):
		if v.kubernetes == nil {
			return nil, fmt.Errorf("%w: kubernetes workload identity is not configured", identity.ErrInvalidToken)
		}
		kubeIdentity, err := v.kubernetes.Verify(ctx, strings.TrimPrefix(credential, identity.KubernetesCredentialPrefix))
		if err != nil {
			return nil, err
		}
		return v.fromCredential(ctx, iam.CredentialKubernetesServiceAccount,
			KubernetesIdentifier(kubeIdentity), ClientKubernetes, kubeIdentity.ExpiresAt)
	case iam.IsWorkloadToken(credential):
		c, err := v.credentials.GetBySecretHash(ctx, iam.HashWorkloadToken(credential))
		if errors.Is(err, iam.ErrNotFound) {
			return nil, fmt.Errorf("%w: unknown workload token", identity.ErrInvalidToken)
		}
		if err != nil {
			return nil, fmt.Errorf("find workload token: %w", err)
		}
		return v.fromCredentialRecord(ctx, c, ClientBearer, time.Time{})
	}
	return v.next.Verify(ctx, credential)
}

func (v *verifier) fromCredential(ctx context.Context, kind iam.WorkloadCredentialKind, identifier, clientID string, expiresAt time.Time) (*identity.Claims, error) {
	c, err := v.credentials.GetByKindAndIdentifier(ctx, kind, identifier)
	if errors.Is(err, iam.ErrNotFound) {
		return nil, fmt.Errorf("%w: unknown workload %s credential", identity.ErrInvalidToken, kind)
	}
	if err != nil {
		return nil, fmt.Errorf("find workload credential: %w", err)
	}
	return v.fromCredentialRecord(ctx, c, clientID, expiresAt)
}

func (v *verifier) fromCredentialRecord(ctx context.Context, c *iam.WorkloadCredential, clientID string, tokenExpiry time.Time) (*identity.Claims, error) {
	if err := c.CheckUsable(); err != nil {
		if errors.Is(err, iam.ErrExpired) {
			return nil, fmt.Errorf("%w: %w", identity.ErrTokenExpired, err)
		}
		return nil, fmt.Errorf("%w: %w", identity.ErrInvalidToken, err)
	}
	w, err := v.workloads.Get(ctx, c.OrganizationID(), c.PrincipalID())
	if errors.Is(err, iam.ErrNotFound) {
		return nil, fmt.Errorf("%w: the workload no longer exists", identity.ErrInvalidToken)
	}
	if err != nil {
		return nil, fmt.Errorf("find workload: %w", err)
	}
	if !w.Active() {
		return nil, fmt.Errorf("%w: the workload is %s", identity.ErrInvalidToken, w.Status())
	}
	now := clock.Now()
	if w.LastSeenAt() == nil || now.Sub(*w.LastSeenAt()) >= lastSeenInterval {
		w.Seen()
		_ = v.workloads.Write(ctx, w)
	}
	// The claims expire when the credential that was presented does: the
	// certificate for mTLS, and whichever of the token and the record ends
	// first otherwise. mTLS renewal needs the certificate's expiry, not the
	// record's, so it can extend the record while the certificate lasts.
	expiresAt := tokenExpiry
	if expiresAt.IsZero() || c.Kind() != iam.CredentialMTLSCertificate {
		if c.NotAfter() != nil && (expiresAt.IsZero() || c.NotAfter().Before(expiresAt)) {
			expiresAt = *c.NotAfter()
		}
	}
	return &identity.Claims{
		Subject:        c.PrincipalID(),
		OrganizationID: c.OrganizationID(),
		ClientID:       clientID,
		Kind:           identity.ActorWorkload,
		ID:             c.ID(),
		IssuedAt:       c.CreatedAt(),
		ExpiresAt:      expiresAt,
	}, nil
}

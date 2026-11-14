package enrollment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/workloadauth"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type ServiceOption func(*options)

type options struct {
	workloads     iam.WorkloadRepository
	tokens        iam.WorkloadEnrollmentTokenRepository
	credentials   iam.WorkloadCredentialRepository
	principals    iam.PrincipalRepository
	clusters      Clusters
	kubernetes    workloadauth.KubernetesVerifier
	authz         authorization.Authorizer
	writer        change.Writer
	enrolTTL      time.Duration
	credentialTTL time.Duration
	grace         time.Duration
}

func WithWorkloadRepo(r iam.WorkloadRepository) ServiceOption {
	return func(o *options) { o.workloads = r }
}
func WithEnrollmentTokenRepo(r iam.WorkloadEnrollmentTokenRepository) ServiceOption {
	return func(o *options) { o.tokens = r }
}
func WithCredentialRepo(r iam.WorkloadCredentialRepository) ServiceOption {
	return func(o *options) { o.credentials = r }
}
func WithPrincipalRepo(r iam.PrincipalRepository) ServiceOption {
	return func(o *options) { o.principals = r }
}
func WithClusters(r Clusters) ServiceOption {
	return func(o *options) { o.clusters = r }
}
func WithKubernetesVerifier(v workloadauth.KubernetesVerifier) ServiceOption {
	return func(o *options) { o.kubernetes = v }
}
func WithAuthorizer(a authorization.Authorizer) ServiceOption {
	return func(o *options) { o.authz = a }
}
func WithTransactor(tx transaction.Transactor) ServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ServiceOption {
	return func(o *options) { o.writer.Audit = r }
}
func WithEnrollmentTokenTTL(d time.Duration) ServiceOption {
	return func(o *options) { o.enrolTTL = d }
}
func WithCredentialTTL(d time.Duration) ServiceOption {
	return func(o *options) { o.credentialTTL = d }
}
func WithGracePeriod(d time.Duration) ServiceOption {
	return func(o *options) { o.grace = d }
}

// NewService returns the enrollment Service, authorizing administrative
// operations and validating its dependencies.
func NewService(opts ...ServiceOption) (Service, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.workloads == nil:
		return nil, errors.New("enrollment: WorkloadRepo is required")
	case o.tokens == nil:
		return nil, errors.New("enrollment: EnrollmentTokenRepo is required")
	case o.credentials == nil:
		return nil, errors.New("enrollment: CredentialRepo is required")
	case o.principals == nil:
		return nil, errors.New("enrollment: PrincipalRepo is required")
	case o.clusters == nil:
		return nil, errors.New("enrollment: Clusters is required")
	case o.authz == nil:
		return nil, errors.New("enrollment: Authorizer is required")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("enrollment: %w", err)
	}
	if o.enrolTTL <= 0 {
		o.enrolTTL = DefaultEnrollmentTokenTTL
	}
	if o.credentialTTL <= 0 {
		o.credentialTTL = DefaultCredentialTTL
	}
	if o.grace <= 0 {
		o.grace = DefaultGracePeriod
	}
	return &authorized{next: &service{options: o}, authz: o.authz}, nil
}

type service struct{ options }

func (s *service) Create(ctx context.Context, actor identity.Actor, in CreateInput) (*IssuedEnrolment, error) {
	if !in.Method.Valid() {
		return nil, delivery.NewErrInvalidInput("credential method must be bearer, mtls, or kubernetes")
	}
	secret, err := iam.NewWorkloadEnrollmentTokenSecret()
	if err != nil {
		return nil, err
	}
	expires := clock.Now().Add(s.enrolTTL)
	pool := "cluster:" + in.ClusterID
	var out *IssuedEnrolment
	err = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so the Cluster cannot be archived while its token is minted.
		cluster, err := s.clusters.GetClusterForUpdate(ctx, in.OrganizationID, in.ClusterID)
		if err != nil {
			return err
		}
		if cluster.Archived() {
			return fmt.Errorf("%w: cluster %s", delivery.ErrArchived, cluster.ID())
		}
		token, err := iam.NewWorkloadEnrollmentToken(iam.NewWorkloadEnrollmentTokenProps{
			OrganizationID: in.OrganizationID, TokenHash: secret.Hash, Pool: pool,
			ClusterID: &in.ClusterID, CredentialKind: in.Method.Kind(),
			CreatedByPrincipalID: actor.PrincipalID, ExpiresAt: expires,
		}, nil)
		if err != nil {
			return err
		}
		if err := s.tokens.Write(ctx, token); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateWorkloadEnrollmentToken, token.ID(), token); err != nil {
			return err
		}
		out = &IssuedEnrolment{ClusterID: cluster.ID(), Method: in.Method, Token: secret.Secret, Pool: pool, ExpiresAt: expires}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: iam.EventWorkloadEnrollmentCreated, OrganizationID: in.OrganizationID,
			ResourceID: token.ID(), Actor: actor, After: token.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) Redeem(ctx context.Context, in RedeemInput) (*Redeemed, error) {
	if !in.Method.Valid() {
		return nil, delivery.NewErrInvalidInput("credential method must be bearer, mtls, or kubernetes")
	}
	if !iam.IsWorkloadEnrollmentToken(in.Token) {
		return nil, delivery.NewErrInvalidInput("not an enrollment token")
	}
	// Verifying a Kubernetes token can fetch the issuer's keys over the
	// network, so it happens before the transaction takes its row locks.
	kube, err := s.verifyKubernetes(ctx, in)
	if err != nil {
		return nil, err
	}
	// A refused redeem rolls back, so the refusal is recorded outside the
	// transaction that refused it, on the Cluster being enrolled.
	outer := ctx
	var conflict *identityInUseError
	var organizationID, clusterID string
	var out *Redeemed
	err = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		token, err := s.tokens.GetByHashForUpdate(ctx, iam.HashWorkloadEnrollmentToken(in.Token))
		if err != nil {
			return err
		}
		switch {
		case token.Used():
			return fmt.Errorf("%w: enrollment token %s", iam.ErrAlreadyUsed, token.ID())
		case token.Expired():
			return fmt.Errorf("%w: enrollment token %s", iam.ErrExpired, token.ID())
		case token.ClusterID() == nil:
			return delivery.NewErrInvalidInput("enrollment token is not bound to a cluster")
		case token.CredentialKind() != in.Method.Kind():
			return delivery.NewErrInvalidInput("enrollment token is for another credential method")
		}
		organizationID, clusterID = token.OrganizationID(), *token.ClusterID()
		// Locked so an archive cannot land between the check and the write.
		cluster, err := s.clusters.GetClusterForUpdate(ctx, organizationID, clusterID)
		if err != nil {
			return err
		}
		if cluster.Archived() {
			return fmt.Errorf("%w: cluster %s", delivery.ErrArchived, cluster.ID())
		}
		workload, err := iam.NewWorkload(iam.NewWorkloadProps{
			PrincipalProps: iam.PrincipalProps{OrganizationID: organizationID, DisplayName: "Runtime agent for " + cluster.DisplayName()},
			Component:      iam.ComponentRuntimeAgent, Pool: token.Pool(),
		}, nil)
		if err != nil {
			return err
		}
		credential, issued, err := s.newCredential(token, workload, in, kube)
		if err != nil {
			return err
		}
		if err := s.revokeSameIdentity(ctx, organizationID, cluster, credential); err != nil {
			var inUse *identityInUseError
			if errors.As(err, &inUse) {
				conflict = inUse
			}
			return err
		}
		if err := s.workloads.Write(ctx, workload); err != nil {
			return err
		}
		if err := s.credentials.Write(ctx, credential); err != nil {
			return err
		}
		if err := token.Redeem(workload); err != nil {
			return err
		}
		if err := s.tokens.Write(ctx, token); err != nil {
			return err
		}
		if err := s.revokeOldAgent(ctx, organizationID, cluster); err != nil {
			return err
		}
		if err := cluster.AssignAgent(workload.ID()); err != nil {
			return err
		}
		if err := s.clusters.UpdateCluster(ctx, cluster); err != nil {
			return err
		}
		if err := s.events(ctx, workload, credential, token, cluster); err != nil {
			return err
		}
		if err := s.auditRecord(ctx, identity.Actor{PrincipalID: workload.ID(), Kind: identity.ActorWorkload},
			iam.EventWorkloadEnrollmentRedeemed, organizationID, token.ID(), credential.Snapshot()); err != nil {
			return err
		}
		out = &Redeemed{
			OrganizationID: organizationID, ClusterID: clusterID,
			WorkloadPrincipalID: workload.ID(), CredentialID: credential.ID(),
			CredentialKind: credential.Kind(), ExpiresAt: credential.NotAfter(),
		}
		if issued != nil {
			out.WorkloadToken = &issued.Secret
		}
		return nil
	})
	if err != nil {
		if conflict != nil {
			s.recordAgentFailure(outer, organizationID, clusterID, conflict.reason())
		}
		return nil, err
	}
	return out, nil
}

// identityInUseError refuses a redeem whose workload identity already serves
// another active Cluster. It names the other Cluster only in the record kept
// for the operator, never in the response: the caller holds only a token.
type identityInUseError struct {
	clusterSlug string
}

func (e *identityInUseError) Error() string {
	return fmt.Sprintf("%s: this workload identity already serves another cluster", delivery.ErrIdentityInUse)
}

func (e *identityInUseError) Is(target error) bool { return target == delivery.ErrIdentityInUse }

func (e *identityInUseError) reason() string {
	return "identity_in_use: this workload identity already serves cluster " + e.clusterSlug
}

// recordAgentFailure notes a refused redeem on the Cluster being enrolled, in
// its own transaction so the refusal survives the rollback. It is best
// effort: the redeem already failed, and its error is what the caller sees.
func (s *service) recordAgentFailure(ctx context.Context, organizationID, clusterID, reason string) {
	_ = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		cluster, err := s.clusters.GetClusterForUpdate(ctx, organizationID, clusterID)
		if err != nil {
			return err
		}
		if err := cluster.RecordAgentFailure(reason); err != nil {
			return err
		}
		return s.clusters.UpdateCluster(ctx, cluster)
	})
}

// verifyKubernetes verifies the projected service-account token of a
// Kubernetes enrolment. It returns nil for the other methods.
func (s *service) verifyKubernetes(ctx context.Context, in RedeemInput) (*workloadauth.KubernetesIdentity, error) {
	if in.Method != MethodKubernetes {
		return nil, nil
	}
	if s.kubernetes == nil {
		return nil, delivery.NewErrInvalidInput("kubernetes workload identity is not configured")
	}
	if in.KubernetesToken == "" {
		return nil, delivery.NewErrInvalidInput("kubernetes enrolment needs a projected service-account token")
	}
	kube, err := s.kubernetes.Verify(ctx, in.KubernetesToken)
	if err != nil {
		return nil, err
	}
	return &kube, nil
}

func (s *service) newCredential(token *iam.WorkloadEnrollmentToken, workload *iam.Workload, in RedeemInput, kube *workloadauth.KubernetesIdentity) (*iam.WorkloadCredential, *iam.IssuedWorkloadToken, error) {
	now := clock.Now()
	switch in.Method {
	case MethodBearer:
		issued, err := iam.NewWorkloadTokenSecret()
		if err != nil {
			return nil, nil, err
		}
		cred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
			OrganizationID: token.OrganizationID(), PrincipalID: workload.ID(),
			Kind: iam.CredentialBearerToken, Identifier: issued.Prefix, SecretHash: issued.Hash,
			NotAfter: ptr(now.Add(s.credentialTTL)),
		}, nil)
		return cred, &issued, err
	case MethodMTLS:
		if in.ClientCertificateFP == "" || in.ClientCertificateEnds.IsZero() {
			return nil, nil, delivery.NewErrInvalidInput("mTLS enrolment needs a client certificate")
		}
		expires := now.Add(s.credentialTTL)
		if in.ClientCertificateEnds.Before(expires) {
			expires = in.ClientCertificateEnds
		}
		cred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
			OrganizationID: token.OrganizationID(), PrincipalID: workload.ID(),
			Kind: iam.CredentialMTLSCertificate, Identifier: in.ClientCertificateFP,
			NotAfter: &expires,
		}, nil)
		return cred, nil, err
	case MethodKubernetes:
		if kube == nil {
			return nil, nil, delivery.NewErrInvalidInput("kubernetes enrolment needs a verified service-account token")
		}
		cred, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
			OrganizationID: token.OrganizationID(), PrincipalID: workload.ID(),
			Kind: iam.CredentialKubernetesServiceAccount, Identifier: workloadauth.KubernetesIdentifier(*kube),
			NotAfter: ptr(now.Add(s.credentialTTL)),
		}, nil)
		return cred, nil, err
	}
	return nil, nil, delivery.NewErrInvalidInput("unknown credential method")
}

func (s *service) Renew(ctx context.Context, caller identity.Caller) (*Renewed, error) {
	if caller.Actor.Kind != identity.ActorWorkload || caller.CredentialID == "" {
		return nil, fmt.Errorf("%w: a workload credential is required", identity.ErrInvalidToken)
	}
	var out *Renewed
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		credential, err := s.credentials.Get(ctx, caller.OrganizationID, caller.CredentialID)
		if err != nil {
			return err
		}
		if credential.PrincipalID() != caller.Actor.PrincipalID {
			return fmt.Errorf("%w: credential does not belong to the workload", identity.ErrInvalidToken)
		}
		if err := credential.CheckUsable(); err != nil {
			return err
		}
		// A superseded credential is accepted only for its grace period so
		// in-flight requests finish; it must not mint another successor.
		if credential.ReplacedBy() != nil {
			return fmt.Errorf("%w: workload credential %s was already renewed", iam.ErrStatus, credential.ID())
		}
		workload, err := s.workloads.Get(ctx, caller.OrganizationID, caller.Actor.PrincipalID)
		if err != nil {
			return err
		}
		if !workload.Active() {
			return fmt.Errorf("%w: workload is %s", iam.ErrStatus, workload.Status())
		}
		if _, err := s.clusters.GetClusterByAgentPrincipalID(ctx, caller.OrganizationID, workload.ID()); err != nil {
			if errors.Is(err, delivery.ErrNotFound) {
				return fmt.Errorf("%w: workload is not a cluster's assigned agent", delivery.ErrNotClusterAgent)
			}
			return err
		}
		previous := credential.NotAfter()
		now := clock.Now()
		// renewed is the credential the workload uses from now on. Its
		// snapshot, not the response, is audited: the response carries the
		// plaintext bearer token.
		renewed := credential
		switch credential.Kind() {
		case iam.CredentialBearerToken:
			issued, err := iam.NewWorkloadTokenSecret()
			if err != nil {
				return err
			}
			next, err := iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
				OrganizationID: caller.OrganizationID, PrincipalID: workload.ID(),
				Kind: iam.CredentialBearerToken, Identifier: issued.Prefix, SecretHash: issued.Hash,
				NotAfter: ptr(now.Add(s.credentialTTL)),
			}, nil)
			if err != nil {
				return err
			}
			if err := s.credentials.Write(ctx, next); err != nil {
				return err
			}
			if err := credential.Supersede(next.ID(), now.Add(s.grace)); err != nil {
				return err
			}
			if err := s.credentials.Write(ctx, credential); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, next.ID(), next); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
				return err
			}
			renewed = next
			out = &Renewed{CredentialID: next.ID(), CredentialKind: next.Kind(), WorkloadToken: &issued.Secret,
				ExpiresAt: next.NotAfter(), PreviousExpiresAt: previous}
		case iam.CredentialMTLSCertificate:
			expires := now.Add(s.credentialTTL)
			if !caller.CredentialExpiresAt.IsZero() && caller.CredentialExpiresAt.Before(expires) {
				expires = caller.CredentialExpiresAt
			}
			if err := credential.Extend(expires); err != nil {
				return err
			}
			if err := s.credentials.Write(ctx, credential); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
				return err
			}
			out = &Renewed{CredentialID: credential.ID(), CredentialKind: credential.Kind(),
				ExpiresAt: credential.NotAfter(), PreviousExpiresAt: previous}
		case iam.CredentialKubernetesServiceAccount:
			if err := credential.Extend(now.Add(s.credentialTTL)); err != nil {
				return err
			}
			if err := s.credentials.Write(ctx, credential); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
				return err
			}
			out = &Renewed{CredentialID: credential.ID(), CredentialKind: credential.Kind(),
				ExpiresAt: credential.NotAfter(), PreviousExpiresAt: previous}
		default:
			return delivery.NewErrInvalidInput("this credential kind cannot be renewed")
		}
		return s.auditRecord(ctx, caller.Actor, iam.EventWorkloadCredentialRenewed,
			caller.OrganizationID, renewed.ID(), renewed.Snapshot())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) Status(ctx context.Context, _ identity.Actor, organizationID, clusterID string) (*AgentStatus, error) {
	cluster, err := s.clusters.GetCluster(ctx, organizationID, clusterID)
	if err != nil {
		return nil, err
	}
	out := &AgentStatus{
		ClusterID:        cluster.ID(),
		AgentPrincipalID: cluster.AgentPrincipalID(),
		LastSeenAt:       cluster.LastReportedAt(),
		LastFailure:      cluster.AgentLastFailure(),
	}
	capabilities := cluster.AgentCapabilities()
	if v, ok := capabilities["agent_version"].(string); ok {
		out.AgentVersion = v
	}
	if v, ok := capabilities["kubernetes_version"].(string); ok {
		out.KubernetesVersion = v
	}
	if cluster.AgentPrincipalID() == nil {
		out.Status = "unassigned"
		return out, nil
	}
	workload, err := s.workloads.Get(ctx, organizationID, *cluster.AgentPrincipalID())
	if err != nil {
		return nil, err
	}
	out.Component = workload.Component()
	enrolledAt := workload.EnrolledAt()
	out.RedeemedAt = &enrolledAt
	if workload.LastSeenAt() != nil {
		out.LastSeenAt = workload.LastSeenAt()
	}
	if workload.Status() != iam.PrincipalActive {
		out.Status = string(workload.Status())
		return out, nil
	}
	credentials, err := s.credentials.ListByPrincipal(ctx, organizationID, workload.ID())
	if err != nil {
		return nil, err
	}
	for i := len(credentials) - 1; i >= 0; i-- {
		c := credentials[i]
		if c.Revoked() {
			continue
		}
		if c.NotAfter() != nil && !clock.Now().Before(*c.NotAfter()) {
			continue
		}
		kind := c.Kind()
		out.CredentialKind = &kind
		out.CredentialExpiresAt = c.NotAfter()
		out.Status = "active"
		return out, nil
	}
	out.Status = "expired"
	return out, nil
}

// revokeSameIdentity revokes the Organization's live credentials that identify
// the same certificate or Kubernetes service account, so re-enrolment can
// register the identity again. Bearer tokens are already unique. An identity
// that already serves another active Cluster is refused instead of revoked:
// silently cutting that Cluster off would leave it naming an agent that can
// no longer authenticate.
func (s *service) revokeSameIdentity(ctx context.Context, organizationID string, enrolling *delivery.Cluster, next *iam.WorkloadCredential) error {
	if next.Kind() == iam.CredentialBearerToken {
		return nil
	}
	previous, err := s.credentials.ListActiveByIdentity(ctx, organizationID, next.Kind(), next.Identifier())
	if err != nil {
		return err
	}
	for _, credential := range previous {
		other, err := s.clusters.GetClusterByAgentPrincipalID(ctx, organizationID, credential.PrincipalID())
		switch {
		case err == nil && other.ID() != enrolling.ID():
			return &identityInUseError{clusterSlug: other.Slug()}
		case err != nil && !errors.Is(err, delivery.ErrNotFound):
			return err
		}
		credential.Revoke()
		if err := s.credentials.Write(ctx, credential); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
			return err
		}
	}
	return nil
}

// revokeOldAgent suspends the Cluster's previous agent and revokes its
// credentials, so only the agent that just enrolled can report.
func (s *service) revokeOldAgent(ctx context.Context, organizationID string, cluster *delivery.Cluster) error {
	if cluster.AgentPrincipalID() == nil {
		return nil
	}
	return revokePrincipal(ctx, s.principals, s.workloads, s.credentials, s.writer, organizationID, *cluster.AgentPrincipalID())
}

func (s *service) events(ctx context.Context, workload *iam.Workload, credential *iam.WorkloadCredential, token *iam.WorkloadEnrollmentToken, cluster *delivery.Cluster) error {
	if err := s.writer.Events(ctx, iam.AggregateWorkload, workload.ID(), workload); err != nil {
		return err
	}
	if err := s.writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
		return err
	}
	if err := s.writer.Events(ctx, iam.AggregateWorkloadEnrollmentToken, token.ID(), token); err != nil {
		return err
	}
	return s.writer.Events(ctx, delivery.AggregateCluster, cluster.ID(), cluster)
}

func (s *service) auditRecord(ctx context.Context, actor identity.Actor, eventType, organizationID, resourceID string, after any) error {
	return s.writer.Audit.Record(ctx, audit.Event{
		Type: eventType, OrganizationID: organizationID, ResourceID: resourceID, Actor: actor, After: after,
	})
}

func ptr[T any](v T) *T { return &v }

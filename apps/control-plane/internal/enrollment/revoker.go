package enrollment

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// agentRevoker suspends a runtime agent's workload and revokes its
// credentials. delivery calls it when it archives a Cluster, in the archive's
// transaction, so no credential outlives the Cluster that named it.
type agentRevoker struct {
	principals  iam.PrincipalRepository
	workloads   iam.WorkloadRepository
	credentials iam.WorkloadCredentialRepository
	writer      change.Writer
}

var _ delivery.AgentRevoker = (*agentRevoker)(nil)

// NewAgentRevoker returns the delivery.AgentRevoker. It opens no transaction
// of its own: it writes through the caller's context, so the workload and
// credential changes and their events commit with the Cluster's archive. Only
// the writer's Outbox is used.
func NewAgentRevoker(
	principals iam.PrincipalRepository,
	workloads iam.WorkloadRepository,
	credentials iam.WorkloadCredentialRepository,
	writer change.Writer,
) (delivery.AgentRevoker, error) {
	switch {
	case principals == nil:
		return nil, errors.New("agent revoker: PrincipalRepo is required")
	case workloads == nil:
		return nil, errors.New("agent revoker: WorkloadRepo is required")
	case credentials == nil:
		return nil, errors.New("agent revoker: CredentialRepo is required")
	case writer.Outbox == nil:
		return nil, errors.New("agent revoker: an Outbox Recorder is required")
	}
	return &agentRevoker{principals: principals, workloads: workloads, credentials: credentials, writer: writer}, nil
}

// RevokeAgent suspends the workload and revokes its live credentials. A
// principal that is gone, or is not a workload, is left alone: there is
// nothing of a runtime agent to revoke.
func (r *agentRevoker) RevokeAgent(ctx context.Context, organizationID, principalID string) error {
	return revokePrincipal(ctx, r.principals, r.workloads, r.credentials, r.writer, organizationID, principalID)
}

// revokePrincipal is RevokeAgent's work, shared with enrollment's replacement
// path, which revokes the agent a Target's previous enrolment left behind.
func revokePrincipal(
	ctx context.Context,
	principals iam.PrincipalRepository,
	workloads iam.WorkloadRepository,
	credentials iam.WorkloadCredentialRepository,
	writer change.Writer,
	organizationID, principalID string,
) error {
	principal, err := principals.Get(ctx, organizationID, principalID)
	if errors.Is(err, iam.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if principal.Kind != iam.PrincipalWorkload {
		return nil
	}
	workload, err := workloads.Get(ctx, organizationID, principal.ID)
	if errors.Is(err, iam.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if workload.Active() {
		if err := workload.Suspend(); err != nil {
			return fmt.Errorf("suspend workload %s: %w", workload.ID(), err)
		}
		if err := workloads.Write(ctx, workload); err != nil {
			return fmt.Errorf("suspend workload %s: %w", workload.ID(), err)
		}
		if err := writer.Events(ctx, iam.AggregateWorkload, workload.ID(), workload); err != nil {
			return err
		}
	}
	live, err := credentials.ListByPrincipal(ctx, organizationID, workload.ID())
	if err != nil {
		return err
	}
	for _, credential := range live {
		if credential.Revoked() {
			continue
		}
		credential.Revoke()
		if err := credentials.Write(ctx, credential); err != nil {
			return fmt.Errorf("revoke credential %s: %w", credential.ID(), err)
		}
		if err := writer.Events(ctx, iam.AggregateWorkloadCredential, credential.ID(), credential); err != nil {
			return err
		}
	}
	return nil
}

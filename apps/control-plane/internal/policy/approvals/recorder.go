package approvals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// recorder opens and cancels requests for the capabilities whose subjects
// wait, in their transactions.
type recorder struct {
	options
}

var _ policy.Approvals = (*recorder)(nil)

func (a *recorder) Open(ctx context.Context, in policy.OpenApprovalInput) (*policy.ApprovalRequest, error) {
	organizationID, actor := in.Request.OrganizationID, in.Request.Actor
	summary, err := json.Marshal(in.Summary)
	if err != nil {
		return nil, fmt.Errorf("encode the approval summary: %w", err)
	}
	digest, err := authorization.ProposalDigest(in.Request.Proposal)
	if err != nil {
		return nil, err
	}
	requirements, err := a.withPaths(ctx, organizationID, in.Requirements)
	if err != nil {
		return nil, err
	}
	props := policy.NewApprovalRequestProps{
		OrganizationID: organizationID, Subject: in.Subject, Decision: in.Decision,
		Action: in.Request.Action, Permission: in.Request.Permission, ResourceID: in.Request.ResourceID,
		ProposalDigest: digest, Summary: summary, ReadResourceID: in.ReadResourceID, OwnerTeamID: in.OwnerTeamID,
		RequestedByPrincipalID: actor.PrincipalID, Requirements: requirements,
	}
	if actor.IsAgent() {
		session, err := a.sessions.Get(ctx, organizationID, actor.PrincipalID)
		if err != nil {
			return nil, fmt.Errorf("read agent session %s: %w", actor.PrincipalID, err)
		}
		props.SessionID = new(session.SessionID())
		props.OnBehalfOfPrincipalID = new(session.DelegatorPrincipalID())
	}
	r, err := policy.NewApprovalRequest(props, nil)
	if err != nil {
		return nil, err
	}
	if err := a.requests.CreateApprovalRequest(ctx, r); err != nil {
		return nil, fmt.Errorf("create approval request for %s %s: %w", in.Subject.Type, in.Subject.ID, err)
	}
	if err := a.save(ctx, actor, r, in.Decision, policy.EventApprovalRequestCreated, nil); err != nil {
		return nil, err
	}
	return r, nil
}

// withPaths returns the requirements with their resource paths read from
// IAM.
func (a *recorder) withPaths(ctx context.Context, organizationID string, requirements []policy.Requirement) ([]policy.Requirement, error) {
	out := make([]policy.Requirement, len(requirements))
	for i, req := range requirements {
		path, err := a.ancestry.ResourceAncestors(ctx, organizationID, req.ResourceID)
		if err != nil {
			return nil, fmt.Errorf("read the path of resource %s: %w", req.ResourceID, err)
		}
		out[i] = policy.Requirement{Permission: req.Permission, ResourceID: req.ResourceID, ResourcePath: path}
	}
	return out, nil
}

// Cancel closes the subject's pending request under the decision the
// context carries for the subject, the one that allowed what stopped it
// waiting.
func (a *recorder) Cancel(ctx context.Context, actor identity.Actor, organizationID string, subject policy.Subject, note string) error {
	decision, ok := authorization.DecisionFor(ctx, subject.ID)
	if !ok {
		return change.ErrUnauthorized
	}
	r, err := a.requests.GetPendingApprovalRequestForUpdate(ctx, organizationID, subject)
	if err != nil {
		return fmt.Errorf("read the pending approval request for %s %s: %w", subject.Type, subject.ID, err)
	}
	before := r.Snapshot()
	if err := r.Cancel(actor.PrincipalID, decision.ID, note); err != nil {
		return err
	}
	if err := a.requests.UpdateApprovalRequest(ctx, r); err != nil {
		return fmt.Errorf("update approval request %s: %w", r.ID(), err)
	}
	return a.save(ctx, actor, r, decision, policy.EventApprovalRequestCancelled, &before)
}

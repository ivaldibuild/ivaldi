package policy

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

// ApprovalRequestReader reads approval requests for the authorizer's
// rules. ApprovalRequestRepository implements it.
type ApprovalRequestReader interface {
	GetApprovalRequest(ctx context.Context, organizationID, requestID string) (*ApprovalRequest, error)
}

// NewAuthorizer returns the single authorizer (ADR 0003): IAM's, which
// checks grants, followed by policy's built-in rules once IAM allows.
// Callers still make one call.
func NewAuthorizer(iam authorization.Authorizer, requests ApprovalRequestReader) authorization.Authorizer {
	return &authorizer{iam: iam, requests: requests}
}

type authorizer struct {
	iam      authorization.Authorizer
	requests ApprovalRequestReader
}

func (a *authorizer) Authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	d, err := a.iam.Authorize(ctx, req)
	if err != nil || d.Outcome == authorization.OutcomeDeny {
		return d, err
	}
	switch {
	case req.Action == ActionApprovalApprove, req.Action == ActionApprovalReject:
		return a.approver(ctx, req, d)
	case req.Actor.IsAgent():
		return agentSessionRules(req, d), nil
	}
	return d, nil
}

// agentSessionRules applies the built-in rules for agent Sessions, which
// IAM allowed with their delegator's access narrowed to their scope. A
// Session may not administer access, credentials, or the Organization, and
// a Deployment it requests waits for a person (ADR 0007). The rule matches
// the permission as well as the action: requesting a Deployment also checks
// application.read under that action, and that check must not wait.
func agentSessionRules(req authorization.Request, d authorization.Decision) authorization.Decision {
	switch {
	case agentSessionAdministration[req.Permission]:
		d.Outcome, d.Rule = authorization.OutcomeDeny, RuleAgentSessionAdministration
		d.Reason = "an agent session cannot manage access, credentials, or the organization"
	case req.Permission == permissionDeploymentCreate && req.Action == actionDeploymentCreate:
		d.Outcome, d.Rule = authorization.OutcomeRequiresApproval, RuleAgentSessionDeployment
		d.Reason = "a deployment requested by an agent session waits for a person's approval"
	}
	return d
}

// approver applies RuleApprover to someone deciding an approval request,
// whom IAM found to hold one of its requirements: only a human user may
// decide, and never one who asked for it, directly or through an agent
// Session acting for them (ADR 0007). Deciding can never itself wait for
// approval. An approval's decision names the approver, so what the
// approved subject records says who let it through.
func (a *authorizer) approver(ctx context.Context, req authorization.Request, d authorization.Decision) (authorization.Decision, error) {
	refuse := func(reason string) (authorization.Decision, error) {
		d.Outcome, d.Rule, d.Reason = authorization.OutcomeDeny, RuleApprover, reason
		return d, nil
	}
	if d.Outcome != authorization.OutcomeAllow {
		return refuse("deciding an approval request cannot itself wait for approval")
	}
	if !req.Actor.IsUser() {
		return refuse("only a human user can decide an approval request")
	}
	proposal, ok := req.Proposal.(DecisionProposal)
	if !ok {
		return authorization.Decision{}, fmt.Errorf("authorize %s: the proposal must be a DecisionProposal, not %T", req.Action, req.Proposal)
	}
	r, err := a.requests.GetApprovalRequest(ctx, req.OrganizationID, proposal.ApprovalRequestID)
	if err != nil {
		return authorization.Decision{}, fmt.Errorf("authorize %s: read approval request: %w", req.Action, err)
	}
	if r.RequestedBy(req.Actor.PrincipalID) {
		return refuse("whoever asked for an approval, directly or through an agent session, cannot decide it")
	}
	if req.Action == ActionApprovalApprove {
		d.ApprovedByPrincipalID = req.Actor.PrincipalID
	}
	return d, nil
}

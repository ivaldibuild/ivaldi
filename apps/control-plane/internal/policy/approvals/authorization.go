package approvals

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context.
//
// Listing needs approval.read on the Organization, which every member
// holds; the list itself shows only what the caller could decide. Reading
// one request needs approval.read where its subject is read. Deciding needs
// no permission of its own: the decider asks for approval.approve or
// approval.reject with each of the request's requirements, so IAM checks
// they could do what was asked themselves, and policy's approver rule
// refuses anyone else (policy.NewAuthorizer).
type authorized struct {
	next     policy.ApprovalService
	authz    authorization.Authorizer
	requests policy.ApprovalRequestRepository
}

var _ policy.ApprovalService = (*authorized)(nil)

func (a *authorized) ListApprovalRequests(ctx context.Context, actor identity.Actor, in policy.ListApprovalRequestsInput) (*pagination.Result[policy.ApprovalRequestView], error) {
	if _, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: policy.PermissionApprovalRead, OrganizationID: in.OrganizationID,
		ResourceID: in.OrganizationID, Action: policy.ActionApprovalList,
	}); err != nil {
		return nil, err
	}
	return a.next.ListApprovalRequests(ctx, actor, in)
}

func (a *authorized) GetApprovalRequest(ctx context.Context, actor identity.Actor, organizationID, requestID string) (*policy.ApprovalRequestView, error) {
	r, err := a.requests.GetApprovalRequest(ctx, organizationID, requestID)
	if err != nil {
		return nil, err
	}
	if _, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: policy.PermissionApprovalRead, OrganizationID: organizationID,
		ResourceID: r.ReadResourceID(), Action: policy.ActionApprovalRead,
	}); err != nil {
		return nil, err
	}
	return a.next.GetApprovalRequest(ctx, actor, organizationID, requestID)
}

func (a *authorized) ApproveRequest(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput) (*policy.ApprovalRequestView, error) {
	ctx, err := a.decide(ctx, actor, in, policy.ActionApprovalApprove)
	if err != nil {
		return nil, err
	}
	return a.next.ApproveRequest(ctx, actor, in)
}

func (a *authorized) RejectRequest(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput) (*policy.ApprovalRequestView, error) {
	ctx, err := a.decide(ctx, actor, in, policy.ActionApprovalReject)
	if err != nil {
		return nil, err
	}
	return a.next.RejectRequest(ctx, actor, in)
}

// decide asks about the action once for each requirement, and returns ctx
// carrying the decision for the requirement the waiting request itself
// asked for, which the request and its subject record.
func (a *authorized) decide(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput, action string) (context.Context, error) {
	r, err := a.requests.GetApprovalRequest(ctx, in.OrganizationID, in.RequestID)
	if err != nil {
		return nil, err
	}
	var recorded authorization.Decision
	for i, req := range r.Requirements() {
		d, err := authorization.Require(ctx, a.authz, authorization.Request{
			Actor: actor, Permission: req.Permission, OrganizationID: in.OrganizationID, ResourceID: req.ResourceID,
			Action: action, Proposal: policy.DecisionProposal{ApprovalRequestID: r.ID()},
		})
		if err != nil {
			return nil, err
		}
		if i == 0 || (req.Permission == r.Permission() && req.ResourceID == r.ResourceID()) {
			recorded = d
		}
	}
	return authorization.WithDecision(ctx, "", recorded), nil
}

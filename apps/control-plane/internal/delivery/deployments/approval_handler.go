package deployments

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// approvalHandler applies or refuses a Deployment when the approval
// request it waits for is decided. Policy calls it in the transaction that
// decides the request, whose context carries the decision for the
// Deployment's ID.
type approvalHandler struct {
	*service
}

var _ policy.SubjectHandler = (*approvalHandler)(nil)

func (h *approvalHandler) Lock(ctx context.Context, organizationID, deploymentID string) error {
	_, err := h.deployments.GetDeploymentForUpdate(ctx, organizationID, deploymentID)
	return err
}

// Approve runs the deploy plan's checks again and, if they pass, moves the
// Deployment to applying under the approval's decision. If one fails, the
// approval is refused and the request stays pending, to be rejected.
func (h *approvalHandler) Approve(ctx context.Context, actor identity.Actor, r *policy.ApprovalRequest) error {
	d, decision, err := h.waiting(ctx, r)
	if err != nil {
		return err
	}
	rel, err := h.releases.GetRelease(ctx, d.OrganizationID(), d.ReleaseID())
	if err != nil {
		return err
	}
	p, err := h.recheck(ctx, rel, d)
	if err == nil {
		err = firstFailure(p.checks)
	}
	if planRefused(err) {
		return fmt.Errorf("%w: %w", policy.ErrCannotApply, err)
	}
	if err != nil {
		return err
	}
	before := d.Snapshot()
	if err := d.Approve(decision.ID); err != nil {
		return err
	}
	return h.saveStatus(ctx, actor, d, before, delivery.EventDeploymentApproved)
}

// Reject moves the Deployment to rejected, which frees its Target.
func (h *approvalHandler) Reject(ctx context.Context, actor identity.Actor, r *policy.ApprovalRequest) error {
	d, _, err := h.waiting(ctx, r)
	if err != nil {
		return err
	}
	before := d.Snapshot()
	if err := d.MoveTo(release.DeploymentRejected); err != nil {
		return err
	}
	return h.saveStatus(ctx, actor, d, before, delivery.EventDeploymentRejected)
}

// waiting returns the Deployment the request is for, which Lock locked,
// and the decision that decided the request.
func (h *approvalHandler) waiting(ctx context.Context, r *policy.ApprovalRequest) (*delivery.Deployment, authorization.Decision, error) {
	subject := r.Subject()
	decision, ok := authorization.DecisionFor(ctx, subject.ID)
	if !ok {
		return nil, authorization.Decision{}, change.ErrUnauthorized
	}
	d, err := h.deployments.GetDeploymentForUpdate(ctx, r.OrganizationID(), subject.ID)
	if err != nil {
		return nil, authorization.Decision{}, err
	}
	if d.Status() != release.DeploymentAwaitingApproval {
		return nil, authorization.Decision{}, fmt.Errorf("%w: deployment %s is %s", policy.ErrCannotApply, d.ID(), d.Status())
	}
	return d, decision, nil
}

// planRefused reports whether err is a deploy plan check failing, as
// firstFailure and the plan's refusals report them, rather than an error
// reading what the plan checks.
func planRefused(err error) bool {
	return errors.Is(err, delivery.ErrInvalid) || errors.Is(err, delivery.ErrDeploymentInFlight) ||
		errors.Is(err, delivery.ErrReleaseStatus) || errors.Is(err, delivery.ErrArchived)
}

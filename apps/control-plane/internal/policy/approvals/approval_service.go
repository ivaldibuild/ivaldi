package approvals

import (
	"context"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// service lists, reads, and decides requests once they are authorized.
type service struct {
	options
}

var _ policy.ApprovalService = (*service)(nil)

// ListApprovalRequests lists the requests the actor could decide: it holds
// every requirement, and neither it nor a Session acting for it asked. Only
// users decide, so anyone else gets an empty page.
func (s *service) ListApprovalRequests(ctx context.Context, actor identity.Actor, in policy.ListApprovalRequestsInput) (*pagination.Result[policy.ApprovalRequestView], error) {
	if in.State == "" {
		in.State = policy.ApprovalPending
	}
	if !in.State.Valid() {
		return nil, policy.NewErrInvalidInput(fmt.Sprintf("approval state %q is not known", in.State))
	}
	if in.Page.SortOrder == "" {
		in.Page.SortOrder = pagination.SortDescending
	}
	if !actor.IsUser() {
		return pagination.NewResult([]policy.ApprovalRequestView{}, 0, false, false, nil, nil), nil
	}
	permissions, err := s.requests.ListRequiredPermissions(ctx, in.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list required permissions: %w", err)
	}
	held := make(map[string][]string, len(permissions))
	for _, permission := range permissions {
		if held[permission], err = s.heldResources(ctx, actor.PrincipalID, in.OrganizationID, permission); err != nil {
			return nil, err
		}
	}
	page, err := s.requests.ListDecidableApprovalRequests(ctx, policy.ListDecidableQuery{
		OrganizationID: in.OrganizationID, DeciderID: actor.PrincipalID, Held: held,
		State: in.State, SubjectType: in.SubjectType, OwnerTeamID: in.OwnerTeamID, Page: in.Page,
	})
	if err != nil {
		return nil, err
	}
	items := make([]policy.ApprovalRequestView, len(page.Items))
	for i, r := range page.Items {
		// The list holds only what the actor could decide, so each pending
		// item can be decided.
		items[i] = policy.ApprovalRequestView{ApprovalRequestSnapshot: r.Snapshot(), CanDecide: r.Pending()}
	}
	return pagination.NewResult(items, page.TotalCount, page.HasNext, page.HasPrevious, page.NextCursor, page.PreviousCursor), nil
}

func (s *service) GetApprovalRequest(ctx context.Context, actor identity.Actor, organizationID, requestID string) (*policy.ApprovalRequestView, error) {
	r, err := s.requests.GetApprovalRequest(ctx, organizationID, requestID)
	if err != nil {
		return nil, err
	}
	canDecide, err := s.canDecide(ctx, actor, r)
	if err != nil {
		return nil, err
	}
	return &policy.ApprovalRequestView{ApprovalRequestSnapshot: r.Snapshot(), CanDecide: canDecide}, nil
}

// canDecide applies the list's filter to one request: it is pending, the
// actor is a user who did not ask for it, and holds every requirement on
// its resource or above.
func (s *service) canDecide(ctx context.Context, actor identity.Actor, r *policy.ApprovalRequest) (bool, error) {
	if !r.Pending() || !actor.IsUser() {
		return false, nil
	}
	return s.userCouldDecide(ctx, actor.PrincipalID, r)
}

// userCouldDecide reports whether a user could decide r: they did not ask
// for it, directly or through a Session acting for them, and hold every
// requirement on its resource or above it.
func (o options) userCouldDecide(ctx context.Context, userID string, r *policy.ApprovalRequest) (bool, error) {
	if r.RequestedBy(userID) {
		return false, nil
	}
	for _, req := range r.Requirements() {
		held, err := o.heldResources(ctx, userID, r.OrganizationID(), req.Permission)
		if err != nil {
			return false, err
		}
		if !slices.ContainsFunc(req.ResourcePath, func(id string) bool { return slices.Contains(held, id) }) {
			return false, nil
		}
	}
	return true, nil
}

func (o options) heldResources(ctx context.Context, principalID, organizationID, permission string) ([]string, error) {
	ids, err := o.held.HeldResources(ctx, iam.HeldResourcesQuery{
		OrganizationID: organizationID, PrincipalID: principalID, Permission: permission, At: clock.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("read where %s holds %s: %w", principalID, permission, err)
	}
	return ids, nil
}

func (s *service) ApproveRequest(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput) (*policy.ApprovalRequestView, error) {
	return s.decide(ctx, actor, in, policy.ApprovalApproved)
}

func (s *service) RejectRequest(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput) (*policy.ApprovalRequestView, error) {
	return s.decide(ctx, actor, in, policy.ApprovalRejected)
}

// decide approves or rejects a pending request and hands its subject to
// the handler its capability registered, in one transaction. The subject
// is locked before the request, as every change to a waiting subject does,
// so a concurrent cancel or decision waits, then finds it no longer pending.
func (s *service) decide(ctx context.Context, actor identity.Actor, in policy.DecideApprovalInput, outcome policy.ApprovalState) (*policy.ApprovalRequestView, error) {
	decision, ok := authorization.DecisionFor(ctx, in.RequestID)
	if !ok {
		return nil, change.ErrUnauthorized
	}
	var decided *policy.ApprovalRequest
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		found, err := s.requests.GetApprovalRequest(ctx, in.OrganizationID, in.RequestID)
		if err != nil {
			return err
		}
		subject := found.Subject()
		subjectType, ok := s.subjects.Lookup(subject.Type)
		if !ok {
			return fmt.Errorf("no capability handles subject type %s", subject.Type)
		}
		if err := subjectType.Handler.Lock(ctx, in.OrganizationID, subject.ID); err != nil {
			return fmt.Errorf("lock %s %s: %w", subject.Type, subject.ID, err)
		}
		r, err := s.requests.GetApprovalRequestForUpdate(ctx, in.OrganizationID, in.RequestID)
		if err != nil {
			return err
		}
		before := r.Snapshot()
		eventType, err := s.close(ctx, actor, r, decision, outcome, in.Note)
		if err != nil {
			return err
		}
		if err := s.requests.UpdateApprovalRequest(ctx, r); err != nil {
			return fmt.Errorf("update approval request %s: %w", r.ID(), err)
		}
		if err := s.save(ctx, actor, r, decision, eventType, &before); err != nil {
			return err
		}
		ctx = authorization.WithDecision(ctx, subject.ID, decision)
		if outcome == policy.ApprovalApproved {
			err = subjectType.Handler.Approve(ctx, actor, r)
		} else {
			err = subjectType.Handler.Reject(ctx, actor, r)
		}
		if err != nil {
			return err
		}
		decided = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &policy.ApprovalRequestView{ApprovalRequestSnapshot: decided.Snapshot()}, nil
}

// close approves or rejects r and returns the audit event type. Work an
// agent Session asked for is not approved once the Session is revoked, so
// a Session cut off for misbehaving cannot ship through someone else's
// approval; a Session that merely ended is fine.
func (s *service) close(ctx context.Context, actor identity.Actor, r *policy.ApprovalRequest, decision authorization.Decision,
	outcome policy.ApprovalState, note string) (string, error) {
	if outcome == policy.ApprovalRejected {
		return policy.EventApprovalRequestRejected, r.Reject(actor.PrincipalID, decision.ID, note)
	}
	if r.Pending() && r.AgentSessionRequested() {
		session, err := s.sessions.Get(ctx, r.OrganizationID(), r.RequestedByPrincipalID())
		if err != nil {
			return "", fmt.Errorf("read agent session %s: %w", r.RequestedByPrincipalID(), err)
		}
		if session.Revoked() {
			return "", policy.ErrSessionRevoked
		}
	}
	return policy.EventApprovalRequestApproved, r.Approve(actor.PrincipalID, decision.ID, note)
}

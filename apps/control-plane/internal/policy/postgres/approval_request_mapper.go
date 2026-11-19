package postgres

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type approvalRequestMapper struct{}

func (approvalRequestMapper) ToPersistence(r *policy.ApprovalRequest) (*approvalRequestEntity, []approvalRequirementEntity) {
	s := r.Snapshot()
	e := &approvalRequestEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, SubjectType: s.SubjectType, SubjectID: s.SubjectID, Title: s.Title,
		State: string(s.State), DecisionID: s.DecisionID, Rule: s.Rule, Reason: s.Reason,
		Action: s.Action, Permission: s.Permission, ResourceID: s.ResourceID, ProposalDigest: s.ProposalDigest,
		Summary: s.Summary, ReadResourceID: s.ReadResourceID, OwnerTeamID: s.OwnerTeamID,
		RequestedByPrincipalID: s.RequestedByPrincipalID, SessionID: s.SessionID, OnBehalfOfPrincipalID: s.OnBehalfOfPrincipalID,
		ClosedByPrincipalID: s.ClosedByPrincipalID, ClosedDecisionID: s.ClosedDecisionID, Note: s.Note, ClosedAt: s.ClosedAt,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	requirements := make([]approvalRequirementEntity, len(s.Requirements))
	for i, req := range s.Requirements {
		requirements[i] = approvalRequirementEntity{
			OrganizationID: s.OrganizationID, ApprovalRequestID: s.ID,
			Permission: req.Permission, ResourceID: req.ResourceID, ResourcePath: database.TextArray(req.ResourcePath),
		}
	}
	return e, requirements
}

func (approvalRequestMapper) ToDomain(e *approvalRequestEntity, requirements []approvalRequirementEntity) (*policy.ApprovalRequest, error) {
	reqs := make([]policy.Requirement, len(requirements))
	for i, r := range requirements {
		reqs[i] = policy.Requirement{Permission: r.Permission, ResourceID: r.ResourceID, ResourcePath: []string(r.ResourcePath)}
	}
	created, updated := e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	var closedAt *time.Time
	if e.ClosedAt != nil {
		closedAt = new(e.ClosedAt.UTC())
	}
	return policy.NewApprovalRequest(policy.NewApprovalRequestProps{
		OrganizationID: e.OrganizationID,
		Subject:        policy.Subject{Type: e.SubjectType, ID: e.SubjectID},
		Title:          e.Title,
		Decision:       authorization.Decision{ID: e.DecisionID, Rule: e.Rule, Reason: e.Reason},
		Action:         e.Action, Permission: e.Permission, ResourceID: e.ResourceID, ProposalDigest: e.ProposalDigest,
		Summary: e.Summary, ReadResourceID: e.ReadResourceID, OwnerTeamID: e.OwnerTeamID,
		RequestedByPrincipalID: e.RequestedByPrincipalID, SessionID: e.SessionID, OnBehalfOfPrincipalID: e.OnBehalfOfPrincipalID,
		Requirements: reqs,
		State:        policy.ApprovalState(e.State), ClosedByPrincipalID: e.ClosedByPrincipalID, ClosedDecisionID: e.ClosedDecisionID,
		Note: e.Note, ClosedAt: closedAt, CreatedAt: &created, UpdatedAt: &updated,
	}, &e.ID)
}

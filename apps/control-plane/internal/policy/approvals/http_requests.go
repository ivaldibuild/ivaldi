package approvals

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type ListApprovalsRequest struct {
	State          string `query:"state" enum:"pending,approved,rejected,cancelled" doc:"Only requests in this state; pending by default"`
	SubjectType    string `query:"subject_type" doc:"Only requests for this kind of subject, such as delivery.deployment"`
	OwnerTeamID    string `query:"owner_team_id" doc:"Only requests routed to this Team"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortOrder      string `query:"sort_order" enum:"asc,desc" doc:"Sort direction by when the request was opened; newest first by default"`
	NextCursor     string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListApprovalsRequest) input(organizationID string) policy.ListApprovalRequestsInput {
	page := pagination.Query{Limit: r.Limit, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return policy.ListApprovalRequestsInput{
		OrganizationID: organizationID, State: policy.ApprovalState(r.State),
		SubjectType: r.SubjectType, OwnerTeamID: r.OwnerTeamID, Page: page,
	}
}

type ApprovalIDRequest struct {
	ApprovalID string `path:"approval_id" doc:"The approval request"`
}

type ApproveRequest struct {
	ApprovalID string `path:"approval_id" doc:"The approval request"`
	Body       struct {
		Note string `json:"note,omitempty" required:"false" maxLength:"2000" doc:"Why it was approved"`
	}
}

type RejectRequest struct {
	ApprovalID string `path:"approval_id" doc:"The approval request"`
	Body       struct {
		Note string `json:"note" required:"true" minLength:"1" maxLength:"2000" doc:"Why it was rejected, for the requester"`
	}
}

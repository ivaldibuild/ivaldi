package policy

// Events an ApprovalRequest records. Each carries the request's snapshot,
// whose owner_team_id lets the capability that notifies people route it.

// ApprovalRequestCreated is recorded when a request is opened.
type ApprovalRequestCreated struct {
	ApprovalRequest ApprovalRequestSnapshot `json:"approval_request"`
}

// ApprovalRequestApproved is recorded when a human approves a request.
type ApprovalRequestApproved struct {
	ApprovalRequest ApprovalRequestSnapshot `json:"approval_request"`
}

// ApprovalRequestRejected is recorded when a human rejects a request.
type ApprovalRequestRejected struct {
	ApprovalRequest ApprovalRequestSnapshot `json:"approval_request"`
}

// ApprovalRequestCancelled is recorded when a request's subject stops
// waiting without a decision.
type ApprovalRequestCancelled struct {
	ApprovalRequest ApprovalRequestSnapshot `json:"approval_request"`
}

func (ApprovalRequestCreated) EventName() string   { return "policy.v1.approval_request.created" }
func (ApprovalRequestApproved) EventName() string  { return "policy.v1.approval_request.approved" }
func (ApprovalRequestRejected) EventName() string  { return "policy.v1.approval_request.rejected" }
func (ApprovalRequestCancelled) EventName() string { return "policy.v1.approval_request.cancelled" }

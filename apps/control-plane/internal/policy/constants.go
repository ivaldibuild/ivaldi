package policy

// Permissions this capability asks the authorizer about. The full catalog,
// and which roles include each one, belongs to IAM: admins hold
// policy.manage, and every role holds policy.read, by IAM's rules.
const (
	PermissionPolicyRead   = "policy.read"
	PermissionPolicyManage = "policy.manage"
	// PermissionApprovalRead lets a principal read the approval requests of
	// subjects it can read. Every role holds it, as a read permission.
	PermissionApprovalRead = "approval.read"
)

// Actions name what an actor asks the authorizer to allow. Policy rules
// and approval requests match on them, and they appear in its decisions.
const (
	ActionSettingLockAdd    = "setting_lock.add"
	ActionSettingLockList   = "setting_lock.list"
	ActionSettingLockRemove = "setting_lock.remove"
	ActionApprovalList      = "approval.list"
	ActionApprovalRead      = "approval.read"
	// ActionApprovalApprove and ActionApprovalReject are asked once for
	// each of a request's requirements, with its permission and resource,
	// so IAM checks the decider holds them.
	ActionApprovalApprove = "approval.approve"
	ActionApprovalReject  = "approval.reject"
)

// Built-in rules policy applies, named in the decisions they make.
const (
	// RuleApprover refuses a decider who is not a human user, or who asked
	// for the approval, directly or through an agent Session.
	RuleApprover = "builtin.approver"
)

// Aggregate types name the domain objects that raise events.
const (
	AggregateSettingLock     = "policy.SettingLock"
	AggregateApprovalRequest = "policy.ApprovalRequest"
)

// Audit event types this capability emits.
const (
	EventSettingLockAdded   = "setting_lock.added"
	EventSettingLockRemoved = "setting_lock.removed"

	EventApprovalRequestCreated   = "approval_request.created"
	EventApprovalRequestApproved  = "approval_request.approved"
	EventApprovalRequestRejected  = "approval_request.rejected"
	EventApprovalRequestCancelled = "approval_request.cancelled"
)

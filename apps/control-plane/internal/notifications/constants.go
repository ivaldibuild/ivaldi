package notifications

// PermissionNotificationRead lets a member read their own notifications.
// Every role holds it, as a read permission.
const PermissionNotificationRead = "notification.read"

// Actions name what an actor asks the authorizer to allow.
const (
	ActionNotificationList = "notification.list"
	ActionNotificationRead = "notification.mark_read"
)

// SubjectApprovalRequest is the subject type of a notification about a
// policy approval request.
const SubjectApprovalRequest = "policy.approval_request"

// Limits on what a notification holds, in characters. Text copied from
// elsewhere is cut to fit with Shorten.
const (
	MaxTitleLength = 255
	MaxBodyLength  = 4000
	maxLinkLength  = 500
)

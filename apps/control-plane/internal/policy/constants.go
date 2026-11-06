package policy

// Permissions this capability asks the authorizer about. The full catalog,
// and which roles include each one, belongs to IAM: admins hold
// policy.manage, and every role holds policy.read, by IAM's rules.
const (
	PermissionPolicyRead   = "policy.read"
	PermissionPolicyManage = "policy.manage"
)

// Actions name what an actor asks the authorizer to allow. Policy rules
// and approval requests match on them, and they appear in its decisions.
const (
	ActionSettingLockAdd    = "setting_lock.add"
	ActionSettingLockList   = "setting_lock.list"
	ActionSettingLockRemove = "setting_lock.remove"
)

// Aggregate types name the domain objects that raise events.
const (
	AggregateSettingLock = "policy.SettingLock"
)

// Audit event types this capability emits.
const (
	EventSettingLockAdded   = "setting_lock.added"
	EventSettingLockRemoved = "setting_lock.removed"
)

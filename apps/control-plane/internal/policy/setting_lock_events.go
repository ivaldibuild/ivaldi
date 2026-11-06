package policy

// Events a SettingLock records. Each carries the lock's snapshot. They tell
// the workspace capability, among others, that which setting values apply
// has changed.

// SettingLockAdded is recorded when a lock is added.
type SettingLockAdded struct {
	SettingLock SettingLockSnapshot `json:"setting_lock"`
}

// SettingLockRemoved is recorded when a lock is removed, so the values below
// its level apply again.
type SettingLockRemoved struct {
	SettingLock          SettingLockSnapshot `json:"setting_lock"`
	RemovedByPrincipalID string              `json:"removed_by_principal_id"`
}

func (SettingLockAdded) EventName() string   { return "policy.v1.setting_lock.added" }
func (SettingLockRemoved) EventName() string { return "policy.v1.setting_lock.removed" }

package workspace

import "encoding/json"

// Events a Setting records. Each carries the Setting as it is after the
// change; the audit record keeps the state before it.

// SettingCreated is recorded when a key gets its first value at a level.
type SettingCreated struct {
	Setting SettingSnapshot `json:"setting"`
}

// SettingChanged is recorded when a key's value at a level changes.
type SettingChanged struct {
	Setting       SettingSnapshot `json:"setting"`
	PreviousValue json.RawMessage `json:"previous_value"`
}

// SettingRemoved is recorded when a key's value is removed from a level, so
// the value from the next level up applies again. It carries the Setting as
// it was, and who removed it under which decision.
type SettingRemoved struct {
	Setting              SettingSnapshot `json:"setting"`
	RemovedByPrincipalID string          `json:"removed_by_principal_id"`
	PolicyDecisionID     string          `json:"policy_decision_id"`
}

func (SettingCreated) EventName() string { return "workspace.v1.setting.created" }
func (SettingChanged) EventName() string { return "workspace.v1.setting.changed" }
func (SettingRemoved) EventName() string { return "workspace.v1.setting.removed" }

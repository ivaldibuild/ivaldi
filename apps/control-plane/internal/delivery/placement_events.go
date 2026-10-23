package delivery

// Events a Placement records. Each carries the Placement as it is after the
// change; the audit record keeps the state before it. Deployments setting
// the current Deployment and Release record their own events.

// PlacementCreated is recorded when an Application is placed on a Target,
// or first observed running there.
type PlacementCreated struct {
	Placement PlacementSnapshot `json:"placement"`
}

// PlacementApplyConfigSet is recorded when how Releases are applied to a
// Placement is set or changed. An observed Placement becomes configured.
type PlacementApplyConfigSet struct {
	Placement PlacementSnapshot `json:"placement"`
}

// PlacementApplyConfigCleared is recorded when a Placement's apply
// configuration is removed, so Ivaldi only tracks Deployments there.
type PlacementApplyConfigCleared struct {
	Placement PlacementSnapshot `json:"placement"`
}

func (PlacementCreated) EventName() string        { return "delivery.v1.placement.created" }
func (PlacementApplyConfigSet) EventName() string { return "delivery.v1.placement.apply_config_set" }
func (PlacementApplyConfigCleared) EventName() string {
	return "delivery.v1.placement.apply_config_cleared"
}

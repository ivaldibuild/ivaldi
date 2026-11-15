package delivery

// Events a Variable records. Each carries the Variable, with its values, as
// it is after the change; the audit record keeps the state before it. Value
// events also name the value.

// VariableCreated is recorded when a Variable is created.
type VariableCreated struct {
	Variable VariableSnapshot `json:"variable"`
}

// VariableUpdated is recorded when a Variable's description or values path changes.
type VariableUpdated struct {
	Variable VariableSnapshot `json:"variable"`
}

// VariableValueAdded is recorded when a value is added to a Variable.
type VariableValueAdded struct {
	Variable VariableSnapshot `json:"variable"`
	ValueID  string           `json:"value_id"`
}

// VariableValueUpdated is recorded when a value's content or scope changes.
type VariableValueUpdated struct {
	Variable VariableSnapshot `json:"variable"`
	ValueID  string           `json:"value_id"`
}

// VariableValueRemoved is recorded when a value is removed. The Variable no
// longer holds it.
type VariableValueRemoved struct {
	Variable VariableSnapshot `json:"variable"`
	ValueID  string           `json:"value_id"`
}

// VariableArchived is recorded when a Variable is archived.
type VariableArchived struct {
	Variable VariableSnapshot `json:"variable"`
}

func (VariableCreated) EventName() string      { return "delivery.v1.variable.created" }
func (VariableUpdated) EventName() string      { return "delivery.v1.variable.updated" }
func (VariableValueAdded) EventName() string   { return "delivery.v1.variable.value_added" }
func (VariableValueUpdated) EventName() string { return "delivery.v1.variable.value_updated" }
func (VariableValueRemoved) EventName() string { return "delivery.v1.variable.value_removed" }
func (VariableArchived) EventName() string     { return "delivery.v1.variable.archived" }

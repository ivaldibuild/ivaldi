package delivery

// Events a revision of a Release's variables records. Each carries the
// revision, with the variables it holds, as it is after the change.

// ReleaseVariablesTaken is recorded when a Release's variables are
// snapshotted: when the Release is created, and at each refresh.
type ReleaseVariablesTaken struct {
	ReleaseVariables ReleaseVariablesSnapshot `json:"release_variables"`
}

// ReleaseVariablesRevisionSuperseded is recorded when a later revision
// replaces this one as its Release's current revision.
type ReleaseVariablesRevisionSuperseded struct {
	ReleaseVariables ReleaseVariablesSnapshot `json:"release_variables"`
}

func (ReleaseVariablesTaken) EventName() string { return "delivery.v1.release_variables.taken" }
func (ReleaseVariablesRevisionSuperseded) EventName() string {
	return "delivery.v1.release_variables.superseded"
}

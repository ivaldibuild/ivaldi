package provenance

// ChangeRecorded is recorded when a Change is recorded.
type ChangeRecorded struct {
	Change ChangeSnapshot `json:"change"`
}

func (ChangeRecorded) EventName() string { return "provenance.v1.change.recorded" }

package versioncontrol

// ConnectionCreated is recorded when an Organization connects a provider
// account.
type ConnectionCreated struct {
	Connection ConnectionSnapshot `json:"connection"`
}

// ConnectionArchived is recorded when a Connection is removed. Its Sources
// remain, but lookups through it stop.
type ConnectionArchived struct {
	Connection ConnectionSnapshot `json:"connection"`
}

func (ConnectionCreated) EventName() string  { return "versioncontrol.v1.connection.created" }
func (ConnectionArchived) EventName() string { return "versioncontrol.v1.connection.archived" }

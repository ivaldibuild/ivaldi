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

// ConnectionSuspended is recorded when the provider suspends a Connection,
// such as when an installation is suspended.
type ConnectionSuspended struct {
	Connection ConnectionSnapshot `json:"connection"`
}

// ConnectionRevoked is recorded when a Connection can no longer be used,
// such as when the provider uninstalls an app.
type ConnectionRevoked struct {
	Connection ConnectionSnapshot `json:"connection"`
}

// ConnectionReactivated is recorded when a suspended Connection becomes
// usable again.
type ConnectionReactivated struct {
	Connection ConnectionSnapshot `json:"connection"`
}

func (ConnectionCreated) EventName() string     { return "versioncontrol.v1.connection.created" }
func (ConnectionArchived) EventName() string    { return "versioncontrol.v1.connection.archived" }
func (ConnectionSuspended) EventName() string   { return "versioncontrol.v1.connection.suspended" }
func (ConnectionRevoked) EventName() string     { return "versioncontrol.v1.connection.revoked" }
func (ConnectionReactivated) EventName() string { return "versioncontrol.v1.connection.reactivated" }

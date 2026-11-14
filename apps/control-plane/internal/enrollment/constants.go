package enrollment

import "time"

// Defaults for the service's durations.
const (
	DefaultEnrollmentTokenTTL = time.Hour
	DefaultCredentialTTL      = 24 * time.Hour
	DefaultGracePeriod        = 5 * time.Minute
)

// Authorizer action names.
const (
	ActionClusterEnrol       = "cluster.enrol"
	ActionClusterAgentStatus = "cluster.agent_status"
)

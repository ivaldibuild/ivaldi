package enrollment

import "time"

// Defaults for the service's durations.
const (
	DefaultEnrollmentTokenTTL = time.Hour
	DefaultCredentialTTL      = 24 * time.Hour
	DefaultGracePeriod        = 5 * time.Minute
)

// Authorizer action names. Creating an enrolment token uses delivery's
// ActionClusterCreateEnrolment, so a policy names one action for it.
const (
	ActionClusterAgentStatus = "cluster.agent_status"
)

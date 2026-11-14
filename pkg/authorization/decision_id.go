package authorization

import "github.com/ivaldibuild/ivaldi/pkg/id"

// DecisionPrefix starts every Decision's ID. Audit records carry the ID, so a
// change can be traced to the decision that allowed it.
const DecisionPrefix = "dcn"

func init() { id.MustRegister(DecisionPrefix, "authorization.Decision") }

// NewDecisionID returns a new Decision ID. The authorizer uses it, and so
// does a service that decides on its own, such as one allowing a runtime
// agent to report on its own Cluster.
func NewDecisionID() string { return id.New(DecisionPrefix) }

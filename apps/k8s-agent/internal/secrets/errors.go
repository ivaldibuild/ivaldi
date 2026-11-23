package secrets

import (
	"errors"
)

// ErrNotApplying is returned when the control plane will grant nothing for
// a Deployment because it is no longer applying: it was cancelled, or
// another agent finished it. The apply is dropped without a report.
var ErrNotApplying = errors.New("the deployment is no longer applying")

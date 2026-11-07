package release

import "fmt"

// DeploymentStatus is where a Deployment is in its life. The control plane
// and the runtime agent share these values and transitions.
type DeploymentStatus string

const (
	DeploymentPending          DeploymentStatus = "pending"
	DeploymentAwaitingApproval DeploymentStatus = "awaiting_approval"
	DeploymentRejected         DeploymentStatus = "rejected"
	DeploymentCancelled        DeploymentStatus = "cancelled"
	DeploymentApplying         DeploymentStatus = "applying"
	DeploymentProgressing      DeploymentStatus = "progressing"
	DeploymentHealthy          DeploymentStatus = "healthy"
	DeploymentDegraded         DeploymentStatus = "degraded"
	DeploymentFailed           DeploymentStatus = "failed"
	DeploymentSuperseded       DeploymentStatus = "superseded"
)

// Valid reports whether s is a known Deployment status.
func (s DeploymentStatus) Valid() bool {
	switch s {
	case DeploymentPending, DeploymentAwaitingApproval, DeploymentRejected, DeploymentCancelled,
		DeploymentApplying, DeploymentProgressing, DeploymentHealthy, DeploymentDegraded,
		DeploymentFailed, DeploymentSuperseded:
		return true
	}
	return false
}

// Terminal reports whether a Deployment in this status can no longer move.
func (s DeploymentStatus) Terminal() bool {
	switch s {
	case DeploymentRejected, DeploymentCancelled, DeploymentFailed, DeploymentSuperseded:
		return true
	}
	return false
}

// deploymentTransitions lists where each Deployment status may go.
var deploymentTransitions = map[DeploymentStatus][]DeploymentStatus{
	DeploymentPending:          {DeploymentAwaitingApproval, DeploymentRejected, DeploymentApplying, DeploymentCancelled},
	DeploymentAwaitingApproval: {DeploymentRejected, DeploymentApplying, DeploymentCancelled},
	DeploymentApplying:         {DeploymentProgressing, DeploymentHealthy, DeploymentDegraded, DeploymentFailed},
	DeploymentProgressing:      {DeploymentHealthy, DeploymentDegraded, DeploymentFailed},
	DeploymentHealthy:          {DeploymentDegraded, DeploymentSuperseded},
	DeploymentDegraded:         {DeploymentHealthy, DeploymentSuperseded},
}

// CanMoveDeployment reports whether a Deployment may go from one status to another.
func CanMoveDeployment(from, to DeploymentStatus) bool {
	for _, s := range deploymentTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// CheckDeploymentMove returns an error unless a Deployment may go from one
// status to another.
func CheckDeploymentMove(from, to DeploymentStatus) error {
	if !CanMoveDeployment(from, to) {
		return fmt.Errorf("a %s deployment cannot become %s", from, to)
	}
	return nil
}

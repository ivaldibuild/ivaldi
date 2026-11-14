// Package status turns the state of an Application's workloads into a
// Deployment status, with kstatus: progressing until they are all current,
// then healthy, failed if a rollout fails, and degraded if a healthy
// Deployment stops being current for longer than a debounce.
package status

import (
	"time"

	"github.com/fluxcd/cli-utils/pkg/kstatus/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Overall is the combined state of an Application's workloads.
type Overall string

const (
	Current    Overall = "current"
	InProgress Overall = "in_progress"
	Failed     Overall = "failed"
)

// Workload is one workload's state, for a report's detail.
type Workload struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Evaluate computes each workload's kstatus and the combined result: failed
// if any failed, in progress if any is not current, current otherwise.
func Evaluate(objects []*unstructured.Unstructured) (Overall, []Workload) {
	overall := Current
	workloads := make([]Workload, 0, len(objects))
	for _, obj := range objects {
		w := Workload{Kind: obj.GetKind(), Name: obj.GetName()}
		result, err := status.Compute(obj)
		switch {
		case err != nil:
			w.Status, w.Message = string(status.UnknownStatus), err.Error()
		default:
			w.Status, w.Message = string(result.Status), result.Message
		}
		switch status.Status(w.Status) {
		case status.CurrentStatus:
		case status.FailedStatus:
			overall = Failed
		default:
			if overall != Failed {
				overall = InProgress
			}
		}
		workloads = append(workloads, w)
	}
	return overall, workloads
}

// Tracker remembers, per Deployment, what it last reported and how long the
// workloads have been unhealthy, and decides each move.
type Tracker struct {
	// Debounce is how long a healthy Deployment's workloads must stay
	// unhealthy before it is reported degraded.
	Debounce time.Duration
	// EmptyGrace is how long a progressing Deployment with no workloads
	// waits, for the caches to catch up, before it counts as healthy.
	EmptyGrace time.Duration

	entries map[string]*entry
}

type entry struct {
	status         release.DeploymentStatus
	firstSeen      time.Time
	unhealthySince time.Time
}

// NewTracker returns a Tracker.
func NewTracker(debounce, emptyGrace time.Duration) *Tracker {
	return &Tracker{Debounce: debounce, EmptyGrace: emptyGrace, entries: map[string]*entry{}}
}

// Next returns the status the Deployment should move to, or "" to stay.
// known is the status the control plane last gave for it, or "" when the
// agent has only its own memory to go on. Next never returns a move
// release.CanMoveDeployment refuses.
func (t *Tracker) Next(deploymentID string, known release.DeploymentStatus, overall Overall, workloads int, now time.Time) release.DeploymentStatus {
	e, ok := t.entries[deploymentID]
	if !ok {
		if known == "" {
			// A placement's current Deployment that the agent has not
			// tracked yet: it has finished its rollout.
			known = release.DeploymentHealthy
		}
		e = &entry{status: known, firstSeen: now}
		t.entries[deploymentID] = e
	} else if known != "" && known != e.status && !release.CanMoveDeployment(known, e.status) {
		// Follow the control plane, unless it is only behind a move the
		// agent has already reported, as an assignment list fetched just
		// before the report is.
		e.status = known
	}

	var next release.DeploymentStatus
	switch e.status {
	case release.DeploymentProgressing:
		switch {
		case overall == Failed:
			next = release.DeploymentFailed
		case overall == Current && (workloads > 0 || now.Sub(e.firstSeen) >= t.EmptyGrace):
			next = release.DeploymentHealthy
		}
	case release.DeploymentHealthy:
		if overall == Current {
			e.unhealthySince = time.Time{}
			break
		}
		if e.unhealthySince.IsZero() {
			e.unhealthySince = now
		}
		if now.Sub(e.unhealthySince) >= t.Debounce {
			next = release.DeploymentDegraded
		}
	case release.DeploymentDegraded:
		if overall == Current {
			next = release.DeploymentHealthy
		}
	}
	if next == "" || !release.CanMoveDeployment(e.status, next) {
		return ""
	}
	return next
}

// Moved records that a move was reported.
func (t *Tracker) Moved(deploymentID string, to release.DeploymentStatus) {
	if e, ok := t.entries[deploymentID]; ok {
		e.status = to
		e.unhealthySince = time.Time{}
	}
}

// Forget forgets a Deployment, so the next Next starts from what it is told.
func (t *Tracker) Forget(deploymentID string) { delete(t.entries, deploymentID) }

// Keep forgets every Deployment not in ids.
func (t *Tracker) Keep(ids map[string]bool) {
	for id := range t.entries {
		if !ids[id] {
			delete(t.entries, id)
		}
	}
}

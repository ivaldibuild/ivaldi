package iam

import (
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// WorkloadComponent is the kind of data-plane component a workload is.
type WorkloadComponent string

const (
	ComponentWorker       WorkloadComponent = "worker"
	ComponentIndexer      WorkloadComponent = "indexer"
	ComponentGateway      WorkloadComponent = "gateway"
	ComponentRuntimeAgent WorkloadComponent = "runtime_agent"
)

// Workload is an enrolled data-plane component in an Organization, such as
// a worker or a runtime agent. It enrolls once with a
// WorkloadEnrollmentToken and then authenticates with WorkloadCredentials.
type Workload struct {
	principal
	component  WorkloadComponent
	pool       string
	enrolledAt time.Time
	lastSeenAt *time.Time
}

// WorkloadSnapshot is a Workload's state, for repositories, audit records,
// and events. Its JSON form is part of the Workload events' contract.
type WorkloadSnapshot struct {
	PrincipalSnapshot
	// Platform is true for a workload that belongs to no Organization; its
	// OrganizationID is then empty.
	Platform   bool              `json:"platform"`
	Component  WorkloadComponent `json:"component"`
	Pool       string            `json:"pool"`
	EnrolledAt time.Time         `json:"enrolled_at"`
	LastSeenAt *time.Time        `json:"last_seen_at"`
}

// NewWorkloadProps describes a Workload. A new one leaves EnrolledAt and
// LastSeenAt nil; a repository loading a stored one sets them.
type NewWorkloadProps struct {
	PrincipalProps
	Component WorkloadComponent
	// Pool names the group of workloads it serves, from its enrollment token.
	Pool       string
	EnrolledAt *time.Time
	LastSeenAt *time.Time
}

// NewWorkload returns a Workload, with a new ID unless principalID gives a stored one.
func NewWorkload(props NewWorkloadProps, principalID *string) (*Workload, error) {
	p, err := newPrincipal(PrincipalWorkload, props.PrincipalProps, principalID)
	if err != nil {
		return nil, err
	}
	if err := validateComponent(props.Component); err != nil {
		return nil, err
	}
	if props.Platform && !isPlatformComponent(props.Component) {
		return nil, NewErrInvalidInput(fmt.Sprintf("a %s cannot be a platform workload", props.Component))
	}
	if err := validateName("pool", props.Pool, maxPoolLength); err != nil {
		return nil, err
	}
	w := &Workload{
		principal:  p,
		component:  props.Component,
		pool:       props.Pool,
		enrolledAt: timeOr(props.EnrolledAt, clock.Now()),
		lastSeenAt: clock.CopyTime(props.LastSeenAt),
	}
	if w.IsNew() {
		w.RecordEvent(WorkloadCreated{w.Snapshot()})
	}
	return w, nil
}

// PlatformPool is the pool of every platform workload, which has no
// enrolment token to name one.
const PlatformPool = "platform"

// NewPlatformWorkload returns a new platform workload: a component that
// serves every Organization, declared in the control plane's configuration
// (ADR 0015).
func NewPlatformWorkload(name string, component WorkloadComponent) (*Workload, error) {
	return NewWorkload(NewWorkloadProps{
		PrincipalProps: PrincipalProps{Platform: true, DisplayName: name},
		Component:      component,
		Pool:           PlatformPool,
	}, nil)
}

func validateComponent(c WorkloadComponent) error {
	switch c {
	case ComponentWorker, ComponentIndexer, ComponentGateway, ComponentRuntimeAgent:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("workload component %q must be worker, indexer, gateway, or runtime_agent", c))
}

func (w *Workload) Component() WorkloadComponent { return w.component }
func (w *Workload) Pool() string                 { return w.pool }
func (w *Workload) EnrolledAt() time.Time        { return w.enrolledAt }
func (w *Workload) LastSeenAt() *time.Time       { return clock.CopyTime(w.lastSeenAt) }

func (w *Workload) Snapshot() WorkloadSnapshot {
	return WorkloadSnapshot{
		PrincipalSnapshot: w.snapshot(),
		Platform:          w.platform,
		Component:         w.component,
		Pool:              w.pool,
		EnrolledAt:        w.enrolledAt,
		LastSeenAt:        clock.CopyTime(w.lastSeenAt),
	}
}

// Seen records that the workload checked in now. It does not change
// UpdatedAt, which tracks changes to the workload itself.
func (w *Workload) Seen() {
	now := clock.Now()
	w.lastSeenAt = &now
}

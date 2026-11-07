package delivery

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// DeploymentEventKind is what a Deployment event reports.
type DeploymentEventKind string

const (
	DeploymentEventStatusChanged DeploymentEventKind = "status_changed"
	DeploymentEventProgress      DeploymentEventKind = "progress"
	DeploymentEventHealth        DeploymentEventKind = "health"
)

// Valid reports whether k is a known Deployment event kind.
func (k DeploymentEventKind) Valid() bool {
	switch k {
	case DeploymentEventStatusChanged, DeploymentEventProgress, DeploymentEventHealth:
		return true
	}
	return false
}

// DeploymentEvent is one append-only entry in a Deployment's history.
type DeploymentEvent struct {
	aggregate.Root
	organizationID string
	deploymentID   string
	kind           DeploymentEventKind
	status         *release.DeploymentStatus
	detail         json.RawMessage
	reportID       *string
	reportedBy     *string
	occurredAt     time.Time
	recordedAt     time.Time
}

// DeploymentEventSnapshot is a DeploymentEvent's state.
type DeploymentEventSnapshot struct {
	ID                    string                    `json:"id"`
	OrganizationID        string                    `json:"organization_id"`
	DeploymentID          string                    `json:"deployment_id"`
	Kind                  DeploymentEventKind       `json:"kind" enum:"status_changed,progress,health"`
	Status                *release.DeploymentStatus `json:"status"`
	Detail                json.RawMessage           `json:"detail"`
	ReportID              *string                   `json:"report_id"`
	ReportedByPrincipalID *string                   `json:"reported_by_principal_id"`
	OccurredAt            time.Time                 `json:"occurred_at"`
	RecordedAt            time.Time                 `json:"recorded_at"`
}

// NewDeploymentEventProps describes a DeploymentEvent.
type NewDeploymentEventProps struct {
	OrganizationID        string
	DeploymentID          string
	Kind                  DeploymentEventKind
	Status                *release.DeploymentStatus
	Detail                json.RawMessage
	ReportID              *string
	ReportedByPrincipalID *string
	OccurredAt            *time.Time
	RecordedAt            *time.Time
}

// NewDeploymentEvent returns a DeploymentEvent, with a new ID unless eventID
// gives a stored one.
func NewDeploymentEvent(props NewDeploymentEventProps, eventID *string) (*DeploymentEvent, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Deployment{}.Prefix(), props.DeploymentID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a deployment ID", props.DeploymentID))
	}
	if !props.Kind.Valid() {
		return nil, NewErrInvalidInput(fmt.Sprintf("deployment event kind %q must be status_changed, progress, or health", props.Kind))
	}
	if props.Status != nil && !props.Status.Valid() {
		return nil, NewErrInvalidInput(fmt.Sprintf("deployment status %q is not known", *props.Status))
	}
	if len(props.Detail) > 0 && !json.Valid(props.Detail) {
		return nil, NewErrInvalidInput("deployment event detail must be valid JSON")
	}
	if props.ReportID != nil {
		if *props.ReportID == "" || len(*props.ReportID) > maxDeploymentReportIDLength {
			return nil, NewErrInvalidInput(fmt.Sprintf("report ID must be 1-%d characters", maxDeploymentReportIDLength))
		}
	}
	if props.ReportedByPrincipalID != nil && !id.Valid("prn", *props.ReportedByPrincipalID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a principal ID", *props.ReportedByPrincipalID))
	}
	root, err := newRoot(DeploymentEvent{}.Prefix(), eventID)
	if err != nil {
		return nil, err
	}
	now := clock.Now()
	e := &DeploymentEvent{
		Root:           root,
		organizationID: props.OrganizationID,
		deploymentID:   props.DeploymentID,
		kind:           props.Kind,
		status:         copyDeploymentStatus(props.Status),
		detail:         append(json.RawMessage(nil), props.Detail...),
		reportID:       optional.OfPtr(props.ReportID),
		reportedBy:     optional.OfPtr(props.ReportedByPrincipalID),
		occurredAt:     timeOr(props.OccurredAt, now),
		recordedAt:     timeOr(props.RecordedAt, now),
	}
	return e, nil
}

func copyDeploymentStatus(s *release.DeploymentStatus) *release.DeploymentStatus {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func (DeploymentEvent) Prefix() string                       { return "dpe" }
func (e *DeploymentEvent) OrganizationID() string            { return e.organizationID }
func (e *DeploymentEvent) DeploymentID() string              { return e.deploymentID }
func (e *DeploymentEvent) Kind() DeploymentEventKind         { return e.kind }
func (e *DeploymentEvent) Status() *release.DeploymentStatus { return copyDeploymentStatus(e.status) }
func (e *DeploymentEvent) ReportID() *string                 { return copyString(e.reportID) }
func (e *DeploymentEvent) ReportedByPrincipalID() *string    { return copyString(e.reportedBy) }
func (e *DeploymentEvent) OccurredAt() time.Time             { return e.occurredAt }
func (e *DeploymentEvent) RecordedAt() time.Time             { return e.recordedAt }

func (e *DeploymentEvent) Snapshot() DeploymentEventSnapshot {
	return DeploymentEventSnapshot{
		ID: e.ID(), OrganizationID: e.organizationID, DeploymentID: e.deploymentID,
		Kind: e.kind, Status: copyDeploymentStatus(e.status), Detail: append(json.RawMessage(nil), e.detail...),
		ReportID: copyString(e.reportID), ReportedByPrincipalID: copyString(e.reportedBy),
		OccurredAt: e.occurredAt, RecordedAt: e.recordedAt,
	}
}

// DeploymentCreated is recorded when a Deployment is requested or observed.
type DeploymentCreated struct {
	Deployment DeploymentSnapshot `json:"deployment"`
}

// DeploymentStatusChanged is recorded whenever a Deployment moves status.
type DeploymentStatusChanged struct {
	Deployment     DeploymentSnapshot       `json:"deployment"`
	PreviousStatus release.DeploymentStatus `json:"previous_status"`
}

func (DeploymentCreated) EventName() string       { return "delivery.v1.deployment.created" }
func (DeploymentStatusChanged) EventName() string { return "delivery.v1.deployment.status_changed" }

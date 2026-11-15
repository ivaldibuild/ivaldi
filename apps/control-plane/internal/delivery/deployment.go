package delivery

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// DeploymentSource says who applied a Deployment.
type DeploymentSource string

const (
	// DeploymentExecuted is a Deployment Ivaldi applied through a Target's agent.
	DeploymentExecuted DeploymentSource = "executed"
	// DeploymentObserved is a Deployment another tool applied, which the agent saw.
	DeploymentObserved DeploymentSource = "observed"
)

// DeploymentReason says why a Deployment was made.
type DeploymentReason string

const (
	DeploymentRollout  DeploymentReason = "rollout"
	DeploymentRollback DeploymentReason = "rollback"
	DeploymentRedeploy DeploymentReason = "redeploy"
)

// Deployment is one Release of an Application applied to one Target. It
// tracks both executed and observed Deployments; v1 creates executed ones.
type Deployment struct {
	aggregate.Root
	organizationID       string
	applicationID        string
	releaseID            string
	environmentID        string
	targetID             string
	source               DeploymentSource
	reason               DeploymentReason
	status               release.DeploymentStatus
	previousDeploymentID *string
	initiatedBy          *string
	sessionID            *string
	workItemID           *string
	externalRef          json.RawMessage
	policyDecisionID     *string
	variableSnapshotID   *string
	findings             []string
	requestedAt          time.Time
	startedAt            *time.Time
	finishedAt           *time.Time
	succeededAt          *time.Time
	statusChangedAt      time.Time
	createdAt            time.Time
	updatedAt            time.Time
}

// DeploymentSnapshot is a Deployment's state, for repositories, audit
// records, and events. Its JSON form is part of the Deployment events'
// contract.
type DeploymentSnapshot struct {
	ID                     string                   `json:"id"`
	OrganizationID         string                   `json:"organization_id"`
	ApplicationID          string                   `json:"application_id"`
	ReleaseID              string                   `json:"release_id"`
	EnvironmentID          string                   `json:"environment_id"`
	TargetID               string                   `json:"target_id"`
	Source                 DeploymentSource         `json:"source" enum:"executed,observed"`
	Reason                 DeploymentReason         `json:"reason" enum:"rollout,rollback,redeploy"`
	Status                 release.DeploymentStatus `json:"status" enum:"pending,awaiting_approval,rejected,cancelled,applying,progressing,healthy,degraded,failed,superseded"`
	PreviousDeploymentID   *string                  `json:"previous_deployment_id"`
	InitiatedByPrincipalID *string                  `json:"initiated_by_principal_id"`
	SessionID              *string                  `json:"session_id"`
	WorkItemID             *string                  `json:"work_item_id"`
	ExternalRef            json.RawMessage          `json:"external_ref"`
	PolicyDecisionID       *string                  `json:"policy_decision_id"`
	// VariableSnapshotID is the revision of the Release's variables an
	// executed Deployment was requested with.
	VariableSnapshotID *string    `json:"variable_snapshot_id"`
	Findings           []string   `json:"findings"`
	RequestedAt        time.Time  `json:"requested_at"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	SucceededAt        *time.Time `json:"succeeded_at"`
	StatusChangedAt    time.Time  `json:"status_changed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// NewDeploymentProps describes a Deployment. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewDeploymentProps struct {
	OrganizationID string
	ApplicationID  string
	ReleaseID      string
	EnvironmentID  string
	TargetID       string
	// Source defaults to executed.
	Source DeploymentSource
	// Reason defaults to rollout.
	Reason                 DeploymentReason
	Status                 release.DeploymentStatus
	PreviousDeploymentID   *string
	InitiatedByPrincipalID *string
	SessionID              *string
	WorkItemID             *string
	ExternalRef            json.RawMessage
	PolicyDecisionID       *string
	VariableSnapshotID     *string
	Findings               []string
	RequestedAt            *time.Time
	StartedAt              *time.Time
	FinishedAt             *time.Time
	SucceededAt            *time.Time
	StatusChangedAt        *time.Time
	CreatedAt              *time.Time
	UpdatedAt              *time.Time
}

const (
	maxDeploymentReferenceLength = 30
	maxDeploymentReportIDLength  = 255
)

// NewDeployment returns a Deployment, with a new ID unless deploymentID
// gives a stored one.
func NewDeployment(props NewDeploymentProps, deploymentID *string) (*Deployment, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{
		"application ID": props.ApplicationID,
		"release ID":     props.ReleaseID,
		"environment ID": props.EnvironmentID,
		"target ID":      props.TargetID,
	} {
		if err := validateRequired(name, value); err != nil {
			return nil, err
		}
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not an application ID", props.ApplicationID))
	}
	if !id.Valid(Release{}.Prefix(), props.ReleaseID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a release ID", props.ReleaseID))
	}
	if !id.Valid(Environment{}.Prefix(), props.EnvironmentID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not an environment ID", props.EnvironmentID))
	}
	if !id.Valid(Target{}.Prefix(), props.TargetID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a target ID", props.TargetID))
	}
	if props.Source == "" {
		props.Source = DeploymentExecuted
	}
	switch props.Source {
	case DeploymentExecuted, DeploymentObserved:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("deployment source %q must be executed or observed", props.Source))
	}
	if props.Reason == "" {
		props.Reason = DeploymentRollout
	}
	switch props.Reason {
	case DeploymentRollout, DeploymentRollback, DeploymentRedeploy:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("deployment reason %q must be rollout, rollback, or redeploy", props.Reason))
	}
	if props.Status == "" {
		props.Status = release.DeploymentPending
	}
	if !props.Status.Valid() {
		return nil, NewErrInvalidInput(fmt.Sprintf("deployment status %q must be pending, awaiting_approval, rejected, cancelled, applying, progressing, healthy, degraded, failed, or superseded", props.Status))
	}
	if props.PreviousDeploymentID != nil && !id.Valid(Deployment{}.Prefix(), *props.PreviousDeploymentID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a deployment ID", *props.PreviousDeploymentID))
	}
	if err := validateDeploymentReference("session ID", props.SessionID); err != nil {
		return nil, err
	}
	if err := validateDeploymentReference("work item ID", props.WorkItemID); err != nil {
		return nil, err
	}
	if props.InitiatedByPrincipalID != nil && !id.Valid("prn", *props.InitiatedByPrincipalID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a principal ID", *props.InitiatedByPrincipalID))
	}
	if props.VariableSnapshotID != nil && !id.Valid(ReleaseVariables{}.Prefix(), *props.VariableSnapshotID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a release variables ID", *props.VariableSnapshotID))
	}
	if props.PolicyDecisionID != nil && !id.Valid(authorization.DecisionPrefix, *props.PolicyDecisionID) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a decision ID", *props.PolicyDecisionID))
	}
	if len(props.ExternalRef) > 0 && !json.Valid(props.ExternalRef) {
		return nil, NewErrInvalidInput("external reference must be valid JSON")
	}
	if err := validateFindings(props.Findings); err != nil {
		return nil, err
	}
	for _, f := range props.Findings {
		if f == "" {
			return nil, NewErrInvalidInput("deployment findings must not be empty")
		}
	}

	now := clock.Now()
	findings := copyStrings(props.Findings)
	if findings == nil {
		findings = []string{}
	}
	d := &Deployment{
		organizationID:       props.OrganizationID,
		applicationID:        props.ApplicationID,
		releaseID:            props.ReleaseID,
		environmentID:        props.EnvironmentID,
		targetID:             props.TargetID,
		source:               props.Source,
		reason:               props.Reason,
		status:               props.Status,
		previousDeploymentID: optional.OfPtr(props.PreviousDeploymentID),
		initiatedBy:          optional.OfPtr(props.InitiatedByPrincipalID),
		sessionID:            optional.OfPtr(props.SessionID),
		workItemID:           optional.OfPtr(props.WorkItemID),
		externalRef:          append(json.RawMessage(nil), props.ExternalRef...),
		policyDecisionID:     optional.OfPtr(props.PolicyDecisionID),
		variableSnapshotID:   optional.OfPtr(props.VariableSnapshotID),
		findings:             findings,
		requestedAt:          timeOr(props.RequestedAt, now),
		startedAt:            clock.CopyTime(props.StartedAt),
		finishedAt:           clock.CopyTime(props.FinishedAt),
		succeededAt:          clock.CopyTime(props.SucceededAt),
		statusChangedAt:      timeOr(props.StatusChangedAt, now),
		createdAt:            timeOr(props.CreatedAt, now),
		updatedAt:            timeOr(props.UpdatedAt, now),
	}
	root, err := newRoot(Deployment{}.Prefix(), deploymentID)
	if err != nil {
		return nil, err
	}
	d.Root = root
	if d.IsNew() {
		d.RecordEvent(DeploymentCreated{d.Snapshot()})
	}
	return d, nil
}

func validateDeploymentReference(name string, value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	if len(*value) > maxDeploymentReferenceLength {
		return NewErrInvalidInput(fmt.Sprintf("%s must be 1-%d characters", name, maxDeploymentReferenceLength))
	}
	return nil
}

func validateFindings(findings []string) error {
	if len(findings) > 20 {
		return NewErrInvalidInput("a deployment can have at most 20 findings")
	}
	return nil
}

func (Deployment) Prefix() string                      { return "dep" }
func (d *Deployment) OrganizationID() string           { return d.organizationID }
func (d *Deployment) ApplicationID() string            { return d.applicationID }
func (d *Deployment) ReleaseID() string                { return d.releaseID }
func (d *Deployment) EnvironmentID() string            { return d.environmentID }
func (d *Deployment) TargetID() string                 { return d.targetID }
func (d *Deployment) Source() DeploymentSource         { return d.source }
func (d *Deployment) Reason() DeploymentReason         { return d.reason }
func (d *Deployment) Status() release.DeploymentStatus { return d.status }
func (d *Deployment) PreviousDeploymentID() *string    { return copyString(d.previousDeploymentID) }
func (d *Deployment) PolicyDecisionID() *string        { return copyString(d.policyDecisionID) }
func (d *Deployment) VariableSnapshotID() *string      { return copyString(d.variableSnapshotID) }
func (d *Deployment) Findings() []string               { return copyStrings(d.findings) }
func (d *Deployment) RequestedAt() time.Time           { return d.requestedAt }
func (d *Deployment) StartedAt() *time.Time            { return clock.CopyTime(d.startedAt) }
func (d *Deployment) FinishedAt() *time.Time           { return clock.CopyTime(d.finishedAt) }
func (d *Deployment) SucceededAt() *time.Time          { return clock.CopyTime(d.succeededAt) }
func (d *Deployment) StatusChangedAt() time.Time       { return d.statusChangedAt }
func (d *Deployment) CreatedAt() time.Time             { return d.createdAt }
func (d *Deployment) UpdatedAt() time.Time             { return d.updatedAt }

func (d *Deployment) Snapshot() DeploymentSnapshot {
	return DeploymentSnapshot{
		ID: d.ID(), OrganizationID: d.organizationID, ApplicationID: d.applicationID, ReleaseID: d.releaseID,
		EnvironmentID: d.environmentID, TargetID: d.targetID, Source: d.source, Reason: d.reason,
		Status: d.status, PreviousDeploymentID: copyString(d.previousDeploymentID),
		InitiatedByPrincipalID: copyString(d.initiatedBy), SessionID: copyString(d.sessionID),
		WorkItemID: copyString(d.workItemID), ExternalRef: append(json.RawMessage(nil), d.externalRef...),
		PolicyDecisionID: copyString(d.policyDecisionID), VariableSnapshotID: copyString(d.variableSnapshotID), Findings: copyStrings(d.findings),
		RequestedAt: d.requestedAt, StartedAt: clock.CopyTime(d.startedAt), FinishedAt: clock.CopyTime(d.finishedAt),
		SucceededAt: clock.CopyTime(d.succeededAt), StatusChangedAt: d.statusChangedAt,
		CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

// MoveTo changes a Deployment's status, applying the shared transition rules
// and recording the status change. Moving to the current status does nothing.
func (d *Deployment) MoveTo(status release.DeploymentStatus) error {
	if status == d.status {
		return nil
	}
	if err := release.CheckDeploymentMove(d.status, status); err != nil {
		return fmt.Errorf("%w: %w", ErrDeploymentStatus, err)
	}
	previous := d.status
	now := clock.Now()
	d.status = status
	d.statusChangedAt = now
	d.updatedAt = now
	if status == release.DeploymentApplying && d.startedAt == nil {
		d.startedAt = &now
	}
	if d.finishedAt == nil && deploymentFinished(status) {
		d.finishedAt = &now
	}
	if status == release.DeploymentHealthy && d.succeededAt == nil {
		d.succeededAt = &now
	}
	d.RecordEvent(DeploymentStatusChanged{Deployment: d.Snapshot(), PreviousStatus: previous})
	return nil
}

func deploymentFinished(status release.DeploymentStatus) bool {
	return slices.Contains([]release.DeploymentStatus{
		release.DeploymentRejected, release.DeploymentCancelled, release.DeploymentHealthy,
		release.DeploymentDegraded, release.DeploymentFailed,
	}, status)
}

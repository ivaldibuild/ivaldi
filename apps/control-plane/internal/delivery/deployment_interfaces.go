package delivery

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// ListDeploymentsQuery lists Deployments in an Application, optionally only
// one Release's or Target's. It sorts by requested_at (the default) or
// status.
type ListDeploymentsQuery struct {
	OrganizationID string
	ApplicationID  string
	ReleaseID      string
	TargetID       string
	Status         release.DeploymentStatus
	Page           pagination.Query
}

// DeploymentRepository stores Deployments and their append-only events.
type DeploymentRepository interface {
	CreateDeployment(ctx context.Context, d *Deployment) error
	// UpdateDeployment writes a Deployment's status and timestamps. Its IDs
	// and trigger never change.
	UpdateDeployment(ctx context.Context, d *Deployment) error
	GetDeployment(ctx context.Context, organizationID, deploymentID string) (*Deployment, error)
	// GetDeploymentForUpdate is GetDeployment that also locks the row until
	// the transaction ends.
	GetDeploymentForUpdate(ctx context.Context, organizationID, deploymentID string) (*Deployment, error)
	ListDeployments(ctx context.Context, query ListDeploymentsQuery) (*pagination.Result[*Deployment], error)
	// GetInFlightDeployment returns the Deployment for the Application and
	// Target that has not finished, or ErrNotFound.
	GetInFlightDeployment(ctx context.Context, organizationID, applicationID, targetID string) (*Deployment, error)
	// ListSucceededEnvironmentIDs returns the Environments where the Release
	// has a Deployment that reached healthy.
	ListSucceededEnvironmentIDs(ctx context.Context, organizationID, releaseID string) ([]string, error)
	// AppendDeploymentEvent inserts the event unless a report with the same
	// ID is already stored, and reports whether it inserted it.
	AppendDeploymentEvent(ctx context.Context, event *DeploymentEvent) (bool, error)
	ListDeploymentEvents(ctx context.Context, organizationID, deploymentID string) ([]*DeploymentEvent, error)
}

// RequestDeploymentsInput asks to deploy one Release to one or more Targets.
type RequestDeploymentsInput struct {
	OrganizationID string
	ReleaseID      string
	TargetIDs      []string
	SessionID      *string
	WorkItemID     *string
}

// DeploymentDetail is a Deployment with its append-only timeline.
type DeploymentDetail struct {
	Deployment DeploymentSnapshot        `json:"deployment"`
	Events     []DeploymentEventSnapshot `json:"events"`
}

// AssignmentArtifact is one artifact and Build the agent needs to apply.
type AssignmentArtifact struct {
	ArtifactID   string       `json:"artifact_id"`
	ArtifactName string       `json:"artifact_name"`
	Kind         ArtifactKind `json:"kind" enum:"container_image,helm_chart,archive"`
	BuildID      string       `json:"build_id"`
	PublishedAs  string       `json:"published_as"`
	Digest       string       `json:"digest"`
}

// DeploymentAssignment is everything a Target's agent needs to execute a
// Deployment.
type DeploymentAssignment struct {
	Deployment      DeploymentSnapshot   `json:"deployment"`
	Release         ReleaseSnapshot      `json:"release"`
	Target          TargetSnapshot       `json:"target"`
	Environment     EnvironmentSnapshot  `json:"environment"`
	ApplyConfig     *ApplyConfig         `json:"apply_config"`
	HelmReleaseName string               `json:"helm_release_name"`
	Artifacts       []AssignmentArtifact `json:"artifacts"`
}

// ArtifactChange is what one artifact changes from on a Target.
type ArtifactChange struct {
	ArtifactID   string         `json:"artifact_id"`
	ArtifactName string         `json:"artifact_name"`
	Previous     *BuildSnapshot `json:"previous"`
	Next         BuildSnapshot  `json:"next"`
}

// DeploymentCheck is one pre-flight check for a Deployment.
type DeploymentCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// ReleaseChanges is what a Release would change on a Target, with the
// checks the deploy sheet needs. Commit-linked Changes and Work Items are
// empty until the version-control and Change capabilities exist.
type ReleaseChanges struct {
	ReleaseID        string            `json:"release_id"`
	TargetID         string            `json:"target_id"`
	CurrentReleaseID *string           `json:"current_release_id"`
	Artifacts        []ArtifactChange  `json:"artifacts"`
	Checks           []DeploymentCheck `json:"checks"`
	Changes          []json.RawMessage `json:"changes"`
}

// ReportDeploymentInput is one agent report about a Deployment.
type ReportDeploymentInput struct {
	OrganizationID string
	DeploymentID   string
	ReportID       string
	Kind           DeploymentEventKind
	Status         *release.DeploymentStatus
	Detail         json.RawMessage
	OccurredAt     *time.Time
}

// DeploymentService requests, reads, cancels, and reports Deployments.
type DeploymentService interface {
	RequestDeployments(ctx context.Context, actor identity.Actor, in RequestDeploymentsInput) ([]*Deployment, error)
	GetDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*DeploymentDetail, error)
	ListDeployments(ctx context.Context, actor identity.Actor, query ListDeploymentsQuery) (*pagination.Result[DeploymentSnapshot], error)
	CancelDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*DeploymentDetail, error)
	// ListAssignments returns the active Deployments for the Target. Only the
	// Target's assigned agent may call it.
	ListAssignments(ctx context.Context, actor identity.Actor, organizationID, targetID string) ([]DeploymentAssignment, error)
	// ReportDeployment records a progress or health report from the Target's
	// assigned agent.
	ReportDeployment(ctx context.Context, actor identity.Actor, in ReportDeploymentInput) (*DeploymentDetail, error)
	// RecordTargetHeartbeat notes that the Target's agent is in contact.
	RecordTargetHeartbeat(ctx context.Context, actor identity.Actor, organizationID, targetID string) error
	// ReleaseChanges returns what the Release would change on the Target and
	// its pre-flight checks.
	ReleaseChanges(ctx context.Context, actor identity.Actor, organizationID, releaseID, targetID string) (*ReleaseChanges, error)
}

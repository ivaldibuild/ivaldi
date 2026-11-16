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
	// ClusterID, when set, keeps only Deployments on the Cluster's active
	// Targets, so an agent's assignments are one query.
	ClusterID string
	Status    release.DeploymentStatus
	// Statuses, when set, keeps only Deployments in one of them.
	Statuses []release.DeploymentStatus
	Page     pagination.Query
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
	// GetLastSucceededDeployment returns the Release's latest Deployment to
	// the Target that reached healthy, executed or observed, or ErrNotFound:
	// what a rollback to the Release replays.
	GetLastSucceededDeployment(ctx context.Context, organizationID, releaseID, targetID string) (*Deployment, error)
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
	// SecretsLatest makes a rollback read the latest secrets instead of
	// the versions the Deployment it restores used. It keeps that
	// Deployment's variables. It has no effect on a rollout or a redeploy,
	// which read the latest anyway.
	SecretsLatest bool
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
// Deployment. The apply block is deployer-neutral: Helm is today's deployer,
// and Argo CD or Flux add a value and a sub-object without changing it.
type DeploymentAssignment struct {
	Deployment  DeploymentSnapshot  `json:"deployment"`
	Release     ReleaseSnapshot     `json:"release"`
	Target      TargetSnapshot      `json:"target"`
	Environment EnvironmentSnapshot `json:"environment"`
	Apply       ApplyAssignment     `json:"apply"`
}

// ArtifactChange is what one artifact changes from on a Target.
type ArtifactChange struct {
	ArtifactID   string         `json:"artifact_id"`
	ArtifactName string         `json:"artifact_name"`
	Previous     *BuildSnapshot `json:"previous"`
	Next         BuildSnapshot  `json:"next"`
	// CommitSHAs are the commits the next Build has and the previous one
	// does not, oldest first. It is empty when the Builds are the same or
	// the range could not be resolved.
	CommitSHAs []string `json:"commit_shas"`
	// RemovedCommitSHAs are the commits the previous Build has and the next
	// one does not, oldest first: what a rollback, or a move to a branch
	// that diverged, takes away.
	RemovedCommitSHAs []string `json:"removed_commit_shas"`
	// Unresolved says why no commit range was listed for this artifact.
	Unresolved []string `json:"unresolved"`
}

// DeploymentCheck is one pre-flight check for a Deployment.
type DeploymentCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// ReleaseChanges is what a Release would change on a Target, with the
// checks the deploy sheet needs. Changes lists the commits the Target would
// gain and RemovedChanges the commits it would lose, once each.
type ReleaseChanges struct {
	ReleaseID        string  `json:"release_id"`
	TargetID         string  `json:"target_id"`
	CurrentReleaseID *string `json:"current_release_id"`
	// Reason is what a Deployment requested now would be: a rollout, a
	// redeploy of the Release that is current there, or a rollback to a
	// Release that was healthy there before.
	Reason DeploymentReason `json:"reason" enum:"rollout,rollback,redeploy"`
	// RestoresDeploymentID is the Deployment a rollback would replay, with
	// its variables and secret versions. A rollback without one found
	// nothing recorded to replay, and uses the Release's current variables
	// and the latest secrets.
	RestoresDeploymentID *string           `json:"restores_deployment_id"`
	Artifacts            []ArtifactChange  `json:"artifacts"`
	Checks               []DeploymentCheck `json:"checks"`
	Changes              []ReleaseChange   `json:"changes"`
	RemovedChanges       []ReleaseChange   `json:"removed_changes"`
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

// ObserveInput is one observation from a Cluster's agent: the images an
// Application is running on a Target, and the workloads they came from.
type ObserveInput struct {
	OrganizationID string
	ClusterID      string
	TargetID       string
	ApplicationID  string
	// Artifacts are the images the agent saw, by published artifact name.
	Artifacts []ObservedArtifact
	// Workloads name what the images came from, for the Deployment's record.
	Workloads []ObservedWorkload
	// ExternalRef names the deployer, when the agent can tell.
	ExternalRef json.RawMessage
	// ReportID makes a retried observation recognizable in the record.
	ReportID   string
	ObservedAt *time.Time
}

// ObservedArtifact is one image the agent saw running, by digest.
type ObservedArtifact struct {
	ArtifactName string `json:"artifact_name"`
	Digest       string `json:"digest"`
}

// ObservedWorkload names a workload an observation came from.
type ObservedWorkload struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  string `json:"revision,omitempty"`
}

// DeploymentService requests, reads, cancels, and reports Deployments.
type DeploymentService interface {
	RequestDeployments(ctx context.Context, actor identity.Actor, in RequestDeploymentsInput) ([]*Deployment, error)
	GetDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*DeploymentDetail, error)
	ListDeployments(ctx context.Context, actor identity.Actor, query ListDeploymentsQuery) (*pagination.Result[DeploymentSnapshot], error)
	CancelDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*DeploymentDetail, error)
	// ListClusterAssignments returns the active Deployments on any of the
	// Cluster's Targets. Only the Cluster's assigned agent may call it.
	ListClusterAssignments(ctx context.Context, actor identity.Actor, organizationID, clusterID string) ([]DeploymentAssignment, error)
	// ReportDeployment records a progress or health report from the Cluster's
	// assigned agent.
	ReportDeployment(ctx context.Context, actor identity.Actor, in ReportDeploymentInput) (*DeploymentDetail, error)
	// Observe records what the Cluster's agent sees running on a Target,
	// creating an observed Deployment when it differs from the current one.
	Observe(ctx context.Context, actor identity.Actor, in ObserveInput) (*DeploymentDetail, error)
	// ReleaseChanges returns what the Release would change on the Target and
	// its pre-flight checks.
	ReleaseChanges(ctx context.Context, actor identity.Actor, organizationID, releaseID, targetID string) (*ReleaseChanges, error)
}

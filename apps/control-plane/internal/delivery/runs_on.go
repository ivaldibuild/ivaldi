package delivery

import (
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// RunsOnQuery asks for a page of a Workspace's Applications with where each
// one runs. It sorts by "created_at" (the default), "slug", or
// "display_name", with the ID breaking ties.
type RunsOnQuery struct {
	OrganizationID string
	WorkspaceID    string
	// ProjectID keeps the read to one Project's Applications.
	ProjectID       string
	OwnerTeamID     string
	IncludeArchived bool
	Page            pagination.Query
}

// PlacementRunsOn is one Target an Application is placed on, and what runs
// there now. It is a read model for "what runs where", not part of the
// Placement aggregate.
type PlacementRunsOn struct {
	ApplicationID string
	TargetID      string
	TargetName    string
	// EnvironmentID and EnvironmentName identify the Target's Environment.
	EnvironmentID   string
	EnvironmentName string
	// ReleaseID and ReleaseVersion are what the placement's current Release
	// says, and DeploymentID and DeploymentStatus what its current Deployment
	// reports. They are empty when nothing is deployed there yet.
	ReleaseID        *string
	ReleaseVersion   *string
	DeploymentID     *string
	DeploymentStatus *release.DeploymentStatus
}

// ApplicationRunsOn is one Application's row in the "what runs where" read:
// its Project and owning Team names, and the Targets it is placed on.
type ApplicationRunsOn struct {
	Application *Application
	// ProjectName is the Application's Project; ProjectOwnerTeamID and
	// ProjectOwnerTeamName are the Team that owns it.
	ProjectName          string
	ProjectOwnerTeamID   string
	ProjectOwnerTeamName string
	// OwnerTeamName is the Team that owns the Application.
	OwnerTeamName string
	Targets       []PlacementRunsOn
}

// RunsOnPage is one page of a Workspace's Applications and the active
// Environments their Targets are in. Environments are returned whole so a
// grid can draw every column, including ones with no placements.
type RunsOnPage struct {
	Applications *pagination.Result[*ApplicationRunsOn]
	Environments []*Environment
}

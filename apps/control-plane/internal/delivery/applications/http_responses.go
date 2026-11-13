package applications

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// CreatedApplication is a new Application with the default Channel created
// with it.
type CreatedApplication struct {
	Application    delivery.ApplicationSnapshot `json:"application"`
	DefaultChannel delivery.ChannelSnapshot     `json:"default_channel"`
}

type CreateApplicationResponse struct {
	Body api.Response[CreatedApplication]
}

type ApplicationResponse struct {
	Body api.Response[delivery.ApplicationSnapshot]
}

type ApplicationListResponse struct {
	Body api.Response[pagination.Result[delivery.ApplicationSnapshot]]
}

// WorkspaceRunsOn is one page of a Workspace's Applications and where each
// one runs. Environments come whole so a grid can draw every column.
type WorkspaceRunsOn struct {
	Items          []RunsOnApplication            `json:"items"`
	TotalCount     int                            `json:"total_count"`
	HasNext        bool                           `json:"has_next"`
	HasPrevious    bool                           `json:"has_previous"`
	NextCursor     *string                        `json:"next_cursor"`
	PreviousCursor *string                        `json:"previous_cursor"`
	Environments   []delivery.EnvironmentSnapshot `json:"environments"`
}

// RunsOnApplication is one Application's row: its Project and owning Team
// names, and the Targets it is placed on.
type RunsOnApplication struct {
	Application delivery.ApplicationSnapshot `json:"application"`
	Project     RunsOnProject                `json:"project"`
	OwnerTeam   RunsOnTeam                   `json:"owner_team"`
	Targets     []RunsOnTarget               `json:"targets"`
}

// RunsOnProject names the Project an Application belongs to and the Team
// that owns it.
type RunsOnProject struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	OwnerTeam   RunsOnTeam `json:"owner_team"`
}

// RunsOnTeam is a Team's ID and display name, without its members.
type RunsOnTeam struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// RunsOnTarget is one Target an Application is placed on, with what runs
// there now. The Release and Deployment are absent when nothing is deployed
// yet.
type RunsOnTarget struct {
	TargetID               string                    `json:"target_id"`
	TargetDisplayName      string                    `json:"target_display_name"`
	EnvironmentID          string                    `json:"environment_id"`
	EnvironmentDisplayName string                    `json:"environment_display_name"`
	ReleaseID              *string                   `json:"release_id"`
	ReleaseVersion         *string                   `json:"release_version"`
	DeploymentID           *string                   `json:"deployment_id"`
	DeploymentStatus       *release.DeploymentStatus `json:"deployment_status" enum:"pending,awaiting_approval,rejected,cancelled,applying,progressing,healthy,degraded,failed,superseded"`
}

type WorkspaceRunsOnResponse struct {
	Body api.Response[WorkspaceRunsOn]
}

func runsOnView(page *delivery.RunsOnPage) WorkspaceRunsOn {
	out := WorkspaceRunsOn{
		Items:          make([]RunsOnApplication, 0, len(page.Applications.Items)),
		TotalCount:     page.Applications.TotalCount,
		HasNext:        page.Applications.HasNext,
		HasPrevious:    page.Applications.HasPrevious,
		NextCursor:     page.Applications.NextCursor,
		PreviousCursor: page.Applications.PreviousCursor,
		Environments:   snapshotAll(page.Environments, (*delivery.Environment).Snapshot),
	}
	for _, row := range page.Applications.Items {
		app := RunsOnApplication{
			Application: row.Application.Snapshot(),
			Project: RunsOnProject{
				ID:          row.Application.ProjectID(),
				DisplayName: row.ProjectName,
				OwnerTeam: RunsOnTeam{
					ID:          row.ProjectOwnerTeamID,
					DisplayName: row.ProjectOwnerTeamName,
				},
			},
			OwnerTeam: RunsOnTeam{
				ID:          row.Application.OwnerTeamID(),
				DisplayName: row.OwnerTeamName,
			},
			Targets: make([]RunsOnTarget, 0, len(row.Targets)),
		}
		for _, target := range row.Targets {
			app.Targets = append(app.Targets, RunsOnTarget{
				TargetID:               target.TargetID,
				TargetDisplayName:      target.TargetName,
				EnvironmentID:          target.EnvironmentID,
				EnvironmentDisplayName: target.EnvironmentName,
				ReleaseID:              target.ReleaseID,
				ReleaseVersion:         target.ReleaseVersion,
				DeploymentID:           target.DeploymentID,
				DeploymentStatus:       target.DeploymentStatus,
			})
		}
		out.Items = append(out.Items, app)
	}
	return out
}

type ArtifactResponse struct {
	Body api.Response[delivery.ArtifactSnapshot]
}

type ArtifactListResponse struct {
	Body api.Response[[]delivery.ArtifactSnapshot]
}

type ChannelResponse struct {
	Body api.Response[delivery.ChannelSnapshot]
}

type ChannelListResponse struct {
	Body api.Response[[]delivery.ChannelSnapshot]
}

type PlacementResponse struct {
	Body api.Response[delivery.PlacementSnapshot]
}

type PlacementListResponse struct {
	Body api.Response[[]delivery.PlacementSnapshot]
}

func applicationResponse(a *delivery.Application) *ApplicationResponse {
	return &ApplicationResponse{Body: api.SuccessResponse(nil, a.Snapshot())}
}

func artifactResponse(a *delivery.Artifact) *ArtifactResponse {
	return &ArtifactResponse{Body: api.SuccessResponse(nil, a.Snapshot())}
}

func channelResponse(c *delivery.Channel) *ChannelResponse {
	return &ChannelResponse{Body: api.SuccessResponse(nil, c.Snapshot())}
}

func placementResponse(p *delivery.Placement) *PlacementResponse {
	return &PlacementResponse{Body: api.SuccessResponse(nil, p.Snapshot())}
}

// snapshots maps a page of domain objects to their snapshots.
func snapshots[T any, S any](r *pagination.Result[T], snapshot func(T) S) pagination.Result[S] {
	return pagination.Result[S]{
		Items:          snapshotAll(r.Items, snapshot),
		TotalCount:     r.TotalCount,
		HasNext:        r.HasNext,
		HasPrevious:    r.HasPrevious,
		NextCursor:     r.NextCursor,
		PreviousCursor: r.PreviousCursor,
	}
}

// snapshotAll maps domain objects to their snapshots, never returning nil.
func snapshotAll[T any, S any](items []T, snapshot func(T) S) []S {
	out := make([]S, 0, len(items))
	for _, it := range items {
		out = append(out, snapshot(it))
	}
	return out
}

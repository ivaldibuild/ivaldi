package projects

import "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"

type CreateProjectRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Project belongs to"`
	Body        struct {
		OwnerTeamID string `json:"owner_team_id" required:"true" minLength:"1" doc:"The Team responsible for the Project"`
		Slug        string `json:"slug" required:"true" doc:"URL slug: lowercase letters, digits, and single hyphens"`
		DisplayName string `json:"display_name" required:"true" minLength:"1" doc:"The Project's display name"`
		Description string `json:"description,omitempty" required:"false" doc:"What the Project is for"`
	}
}

func (r *CreateProjectRequest) input(organizationID string) workspace.CreateProjectInput {
	return workspace.CreateProjectInput{
		OrganizationID: organizationID,
		WorkspaceID:    r.WorkspaceID,
		OwnerTeamID:    r.Body.OwnerTeamID,
		Slug:           r.Body.Slug,
		DisplayName:    r.Body.DisplayName,
		Description:    r.Body.Description,
	}
}

type ListProjectsRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Projects in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Projects"`
}

type ProjectRequest struct {
	ProjectID string `path:"project_id" doc:"The Project's ID"`
}

type UpdateProjectRequest struct {
	ProjectID string `path:"project_id" doc:"The Project's ID"`
	Body      struct {
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"1" doc:"New display name"`
		Description *string `json:"description,omitempty" required:"false" doc:"New description"`
	}
}

func (r *UpdateProjectRequest) props() workspace.UpdateProjectProps {
	return workspace.UpdateProjectProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Description: r.Body.Description}
}

type TransferProjectRequest struct {
	ProjectID string `path:"project_id" doc:"The Project's ID"`
	Body      struct {
		OwnerTeamID string `json:"owner_team_id" required:"true" minLength:"1" doc:"The Team to hand the Project to"`
	}
}

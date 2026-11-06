package workspaces

import "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"

type CreateWorkspaceRequest struct {
	Body struct {
		Slug        string `json:"slug" required:"true" doc:"URL slug: lowercase letters, digits, and single hyphens"`
		DisplayName string `json:"display_name" required:"true" minLength:"1" doc:"The Workspace's display name"`
		Description string `json:"description,omitempty" required:"false" doc:"What the Workspace is for"`
	}
}

func (r *CreateWorkspaceRequest) input(organizationID string) workspace.CreateWorkspaceInput {
	return workspace.CreateWorkspaceInput{
		OrganizationID: organizationID,
		Slug:           r.Body.Slug,
		DisplayName:    r.Body.DisplayName,
		Description:    r.Body.Description,
	}
}

type ListWorkspacesRequest struct {
	IncludeArchived bool `query:"include_archived" doc:"Include archived Workspaces"`
}

type WorkspaceRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace's ID"`
}

type UpdateWorkspaceRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace's ID"`
	Body        struct {
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"1" doc:"New display name"`
		Description *string `json:"description,omitempty" required:"false" doc:"New description"`
	}
}

func (r *UpdateWorkspaceRequest) props() workspace.UpdateWorkspaceProps {
	return workspace.UpdateWorkspaceProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Description: r.Body.Description}
}

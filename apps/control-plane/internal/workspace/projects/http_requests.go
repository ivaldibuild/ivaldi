package projects

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

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
	Search          string `query:"q" maxLength:"255" doc:"Keep the Projects whose name or slug contains this, ignoring case"`
	OwnerTeamID     string `query:"owner_team_id" doc:"Keep the Projects this Team owns"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Projects"`
	Limit           int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy          string `query:"sort_by" enum:"created_at,slug,display_name" doc:"Field to sort by: created_at (the default), slug, or display_name"`
	SortOrder       string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor      string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor  string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListProjectsRequest) query(organizationID string) workspace.ListProjectsQuery {
	page := pagination.Query{Limit: r.Limit, SortBy: r.SortBy, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return workspace.ListProjectsQuery{
		OrganizationID:  organizationID,
		WorkspaceID:     r.WorkspaceID,
		Search:          r.Search,
		OwnerTeamID:     r.OwnerTeamID,
		IncludeArchived: r.IncludeArchived,
		Page:            page,
	}
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

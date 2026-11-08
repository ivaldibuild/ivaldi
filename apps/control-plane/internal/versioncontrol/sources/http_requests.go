package sources

import (
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// PageParams are a list operation's paging parameters.
type PageParams struct {
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy         string `query:"sort_by" enum:"created_at,full_name" doc:"Field to sort by: created_at (the default) or full_name"`
	SortOrder      string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor     string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (p PageParams) query() pagination.Query {
	q := pagination.Query{Limit: p.Limit, SortBy: p.SortBy, SortOrder: p.SortOrder}
	if p.NextCursor != "" {
		q.NextCursor = &p.NextCursor
	}
	if p.PreviousCursor != "" {
		q.PreviousCursor = &p.PreviousCursor
	}
	return q
}

type CreateSourceRequest struct {
	ProjectID string `path:"project_id" doc:"The Project the Source belongs to"`
	Body      struct {
		ConnectionID         string `json:"connection_id" required:"true" doc:"The Connection the repository is reached through"`
		ExternalRepositoryID string `json:"external_repository_id" required:"true" doc:"The provider's stable repository ID"`
	}
}

type ListProjectSourcesRequest struct {
	ProjectID       string `path:"project_id" doc:"The Project to list Sources in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include disconnected Sources"`
	PageParams
}

type ListWorkspaceSourcesRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Sources in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include disconnected Sources"`
	PageParams
}

type SourceRequest struct {
	SourceID string `path:"source_id" doc:"The Source's ID"`
}

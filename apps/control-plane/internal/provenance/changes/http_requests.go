package changes

import (
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// PageParams are a list operation's paging parameters.
type PageParams struct {
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy         string `query:"sort_by" enum:"created_at,title" doc:"Field to sort by: created_at (the default) or title"`
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

type RecordChangeRequest struct {
	SourceID string `path:"source_id" doc:"The Source the commits belong to"`
	Body     struct {
		CommitSHAs  []string `json:"commit_shas" required:"true" minItems:"1" maxItems:"100" doc:"Commits the Change contains, as full SHAs"`
		SessionID   *string  `json:"session_id,omitempty" required:"false" doc:"The Session that produced the change"`
		WorkItemID  *string  `json:"work_item_id,omitempty" required:"false" doc:"The Work Item the change serves"`
		Title       string   `json:"title" required:"true" minLength:"1" maxLength:"512" doc:"Short description of the change"`
		ExternalRef *string  `json:"external_ref,omitempty" required:"false" doc:"Provider reference such as a pull request URL"`
	}
}

type ListChangesRequest struct {
	SourceID string `path:"source_id" doc:"The Source to list Changes for"`
	PageParams
}

type ChangeRequest struct {
	ChangeID string `path:"change_id" doc:"The Change's ID"`
}

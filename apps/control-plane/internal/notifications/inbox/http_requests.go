package inbox

import "github.com/ivaldibuild/ivaldi/pkg/pagination"

type ListNotificationsRequest struct {
	Unread         bool   `query:"unread" doc:"Only notifications not yet read"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortOrder      string `query:"sort_order" enum:"asc,desc" doc:"Sort direction by when it was made; newest first by default"`
	NextCursor     string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

func (r *ListNotificationsRequest) page() pagination.Query {
	page := pagination.Query{Limit: r.Limit, SortOrder: r.SortOrder}
	if r.NextCursor != "" {
		page.NextCursor = &r.NextCursor
	}
	if r.PreviousCursor != "" {
		page.PreviousCursor = &r.PreviousCursor
	}
	return page
}

type NotificationIDRequest struct {
	NotificationID string `path:"notification_id" doc:"The notification"`
}

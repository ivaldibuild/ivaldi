package changes

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type ChangeResponse struct {
	Body api.Response[provenance.ChangeSnapshot]
}

type ChangeListResponse struct {
	Body api.Response[pagination.Result[provenance.ChangeSnapshot]]
}

func changeResponse(c *provenance.Change) *ChangeResponse {
	return &ChangeResponse{Body: api.SuccessResponse(nil, c.Snapshot())}
}

func snapshots(page *pagination.Result[*provenance.Change]) pagination.Result[provenance.ChangeSnapshot] {
	items := make([]provenance.ChangeSnapshot, 0, len(page.Items))
	for _, change := range page.Items {
		items = append(items, change.Snapshot())
	}
	return pagination.Result[provenance.ChangeSnapshot]{
		Items:          items,
		TotalCount:     page.TotalCount,
		HasNext:        page.HasNext,
		HasPrevious:    page.HasPrevious,
		NextCursor:     page.NextCursor,
		PreviousCursor: page.PreviousCursor,
	}
}

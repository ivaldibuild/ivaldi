package clusters

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type ClusterResponse struct {
	Body api.Response[delivery.ClusterSnapshot]
}

type ClusterListResponse struct {
	Body api.Response[pagination.Result[delivery.ClusterSnapshot]]
}

func clusterResponse(c *delivery.Cluster) *ClusterResponse {
	return &ClusterResponse{Body: api.SuccessResponse(nil, c.Snapshot())}
}

// snapshots maps a page of domain objects to their snapshots.
func snapshots[T any, S any](r *pagination.Result[T], snapshot func(T) S) pagination.Result[S] {
	items := make([]S, 0, len(r.Items))
	for _, it := range r.Items {
		items = append(items, snapshot(it))
	}
	return pagination.Result[S]{
		Items:          items,
		TotalCount:     r.TotalCount,
		HasNext:        r.HasNext,
		HasPrevious:    r.HasPrevious,
		NextCursor:     r.NextCursor,
		PreviousCursor: r.PreviousCursor,
	}
}

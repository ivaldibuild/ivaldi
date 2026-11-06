package lifecycles

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type LifecycleResponse struct {
	Body api.Response[delivery.LifecycleSnapshot]
}

type LifecycleListResponse struct {
	Body api.Response[pagination.Result[delivery.LifecycleSnapshot]]
}

func lifecycleResponse(l *delivery.Lifecycle) *LifecycleResponse {
	return &LifecycleResponse{Body: api.SuccessResponse(nil, l.Snapshot())}
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

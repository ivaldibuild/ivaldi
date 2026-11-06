package environments

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type EnvironmentResponse struct {
	Body api.Response[delivery.EnvironmentSnapshot]
}

type EnvironmentListResponse struct {
	Body api.Response[pagination.Result[delivery.EnvironmentSnapshot]]
}

type TargetResponse struct {
	Body api.Response[delivery.TargetSnapshot]
}

type TargetListResponse struct {
	Body api.Response[pagination.Result[delivery.TargetSnapshot]]
}

func environmentResponse(e *delivery.Environment) *EnvironmentResponse {
	return &EnvironmentResponse{Body: api.SuccessResponse(nil, e.Snapshot())}
}

func targetResponse(t *delivery.Target) *TargetResponse {
	return &TargetResponse{Body: api.SuccessResponse(nil, t.Snapshot())}
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

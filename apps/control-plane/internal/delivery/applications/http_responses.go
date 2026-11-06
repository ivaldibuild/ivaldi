package applications

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// CreatedApplication is a new Application with the default Channel created
// with it.
type CreatedApplication struct {
	Application    delivery.ApplicationSnapshot `json:"application"`
	DefaultChannel delivery.ChannelSnapshot     `json:"default_channel"`
}

type CreateApplicationResponse struct {
	Body api.Response[CreatedApplication]
}

type ApplicationResponse struct {
	Body api.Response[delivery.ApplicationSnapshot]
}

type ApplicationListResponse struct {
	Body api.Response[pagination.Result[delivery.ApplicationSnapshot]]
}

type ArtifactResponse struct {
	Body api.Response[delivery.ArtifactSnapshot]
}

type ArtifactListResponse struct {
	Body api.Response[[]delivery.ArtifactSnapshot]
}

type ChannelResponse struct {
	Body api.Response[delivery.ChannelSnapshot]
}

type ChannelListResponse struct {
	Body api.Response[[]delivery.ChannelSnapshot]
}

type PlacementResponse struct {
	Body api.Response[delivery.PlacementSnapshot]
}

type PlacementListResponse struct {
	Body api.Response[[]delivery.PlacementSnapshot]
}

func applicationResponse(a *delivery.Application) *ApplicationResponse {
	return &ApplicationResponse{Body: api.SuccessResponse(nil, a.Snapshot())}
}

func artifactResponse(a *delivery.Artifact) *ArtifactResponse {
	return &ArtifactResponse{Body: api.SuccessResponse(nil, a.Snapshot())}
}

func channelResponse(c *delivery.Channel) *ChannelResponse {
	return &ChannelResponse{Body: api.SuccessResponse(nil, c.Snapshot())}
}

func placementResponse(p *delivery.Placement) *PlacementResponse {
	return &PlacementResponse{Body: api.SuccessResponse(nil, p.Snapshot())}
}

// snapshots maps a page of domain objects to their snapshots.
func snapshots[T any, S any](r *pagination.Result[T], snapshot func(T) S) pagination.Result[S] {
	return pagination.Result[S]{
		Items:          snapshotAll(r.Items, snapshot),
		TotalCount:     r.TotalCount,
		HasNext:        r.HasNext,
		HasPrevious:    r.HasPrevious,
		NextCursor:     r.NextCursor,
		PreviousCursor: r.PreviousCursor,
	}
}

// snapshotAll maps domain objects to their snapshots, never returning nil.
func snapshotAll[T any, S any](items []T, snapshot func(T) S) []S {
	out := make([]S, 0, len(items))
	for _, it := range items {
		out = append(out, snapshot(it))
	}
	return out
}

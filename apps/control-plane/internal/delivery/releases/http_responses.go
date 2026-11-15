package releases

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type BuildResponse struct {
	Body api.Response[delivery.BuildSnapshot]
}

type ReleaseResponse struct {
	Body api.Response[Release]
}

type ReleaseListResponse struct {
	Body api.Response[pagination.Result[Release]]
}

// Release is a Release with each of its Builds in full.
type Release struct {
	delivery.ReleaseSnapshot
	Builds []ReleaseBuild `json:"builds"`
}

// ReleaseBuild is the Build a Release carries for one artifact.
type ReleaseBuild struct {
	ArtifactID string                 `json:"artifact_id"`
	Build      delivery.BuildSnapshot `json:"build"`
}

func releaseView(d *delivery.ReleaseDetail) Release {
	out := Release{ReleaseSnapshot: d.Release.Snapshot(), Builds: []ReleaseBuild{}}
	for i, rb := range d.Release.Builds() {
		item := ReleaseBuild{ArtifactID: rb.ArtifactID}
		if i < len(d.Builds) && d.Builds[i] != nil {
			item.Build = d.Builds[i].Snapshot()
		}
		out.Builds = append(out.Builds, item)
	}
	return out
}

type ReleaseVariablesResponse struct {
	Body api.Response[delivery.ReleaseVariablesView]
}

package teams

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type TeamResponse struct {
	Body api.Response[team.TeamSnapshot]
}

type TeamListResponse struct {
	Body api.Response[pagination.Result[team.TeamSnapshot]]
}

type TeamMemberListResponse struct {
	Body api.Response[[]team.TeamMemberSnapshot]
}

// AgentInTeam is a new Agent and the Team it joined.
type AgentInTeam struct {
	Agent iam.AgentSnapshot `json:"agent"`
	Team  team.TeamSnapshot `json:"team"`
}

type AgentResponse struct {
	Body api.Response[AgentInTeam]
}

func teamResponse(t *team.Team) *TeamResponse {
	return &TeamResponse{Body: api.SuccessResponse(nil, t.Snapshot())}
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

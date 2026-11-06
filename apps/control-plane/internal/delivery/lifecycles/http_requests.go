package lifecycles

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// PageParams are a list operation's paging parameters.
type PageParams struct {
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy         string `query:"sort_by" doc:"Field to sort by"`
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

// PhaseBody is a Lifecycle phase as a request gives it.
type PhaseBody struct {
	Name            string   `json:"name" required:"true" minLength:"1" doc:"The phase's name"`
	Optional        bool     `json:"optional,omitempty" required:"false" doc:"Whether a Release may skip the phase"`
	MinEnvironments *int     `json:"min_environments,omitempty" required:"false" minimum:"1" doc:"How many of the phase's Environments need a successful Deployment before the next phase opens; all when omitted"`
	EnvironmentIDs  []string `json:"environment_ids" required:"true" doc:"The Workspace's active Environments in the phase"`
}

func (p PhaseBody) props() delivery.LifecyclePhaseProps {
	return delivery.LifecyclePhaseProps{Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs}
}

type CreateLifecycleRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Lifecycle belongs to"`
	Body        struct {
		DisplayName string      `json:"display_name" required:"true" minLength:"3" doc:"The display name for the lifecycle"`
		Slug        *string     `json:"slug,omitempty" required:"false" doc:"Url slug for the lifecycle; derived from the display name when omitted"`
		Description *string     `json:"description,omitempty" required:"false" doc:"What the lifecycle is for"`
		Phases      []PhaseBody `json:"phases,omitempty" required:"false" doc:"The phases a Release moves through, in order"`
	}
}

func (r *CreateLifecycleRequest) input(organizationID string) delivery.CreateLifecycleInput {
	in := delivery.CreateLifecycleInput{
		OrganizationID: organizationID,
		WorkspaceID:    r.WorkspaceID,
		DisplayName:    r.Body.DisplayName,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	if r.Body.Description != nil {
		in.Description = *r.Body.Description
	}
	for _, p := range r.Body.Phases {
		in.Phases = append(in.Phases, p.props())
	}
	return in
}

type ListLifecyclesRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Lifecycles in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Lifecycles"`
	PageParams
}

type LifecycleRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
}

type UpdateLifecycleRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
	Body        struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		Description *string `json:"description,omitempty" required:"false" doc:"New description; empty clears it"`
	}
}

func (r *UpdateLifecycleRequest) props() delivery.UpdateLifecycleProps {
	return delivery.UpdateLifecycleProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Description: r.Body.Description}
}

type AddLifecyclePhaseRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
	Body        PhaseBody
}

type LifecyclePhaseRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
	PhaseID     string `path:"phase_id" doc:"The phase's ID"`
}

type UpdateLifecyclePhaseRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
	PhaseID     string `path:"phase_id" doc:"The phase's ID"`
	Body        PhaseBody
}

type MoveLifecyclePhaseRequest struct {
	LifecycleID string `path:"lifecycle_id" doc:"The Lifecycle's ID"`
	PhaseID     string `path:"phase_id" doc:"The phase's ID"`
	Body        struct {
		Position int `json:"position" required:"true" minimum:"0" doc:"The phase's new zero-based position"`
	}
}

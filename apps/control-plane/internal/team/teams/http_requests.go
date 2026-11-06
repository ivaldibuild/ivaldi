package teams

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// PageParams are a list operation's paging parameters.
type PageParams struct {
	Limit          int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy         string `query:"sort_by" doc:"Field to sort by: created_at (the default), slug, or display_name"`
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

type CreateTeamRequest struct {
	Body struct {
		DisplayName string  `json:"display_name" required:"true" minLength:"1" maxLength:"255" doc:"The display name for the team"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"Url slug for the team; derived from the display name when omitted"`
		Description *string `json:"description,omitempty" required:"false" doc:"What the team is responsible for"`
	}
}

func (r *CreateTeamRequest) input(organizationID string) team.CreateTeamInput {
	in := team.CreateTeamInput{
		OrganizationID: organizationID,
		DisplayName:    r.Body.DisplayName,
		Description:    r.Body.Description,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	return in
}

type ListTeamsRequest struct {
	IncludeArchived bool `query:"include_archived" doc:"Include archived Teams"`
	PageParams
}

type TeamRequest struct {
	TeamID string `path:"team_id" doc:"The Team's ID"`
}

type UpdateTeamRequest struct {
	TeamID string `path:"team_id" doc:"The Team's ID"`
	Body   struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"1" maxLength:"255" doc:"New display name"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		Description *string `json:"description,omitempty" required:"false" doc:"New description; empty removes it"`
	}
}

func (r *UpdateTeamRequest) props() team.UpdateTeamProps {
	return team.UpdateTeamProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Description: r.Body.Description}
}

type TeamMemberRequest struct {
	TeamID      string `path:"team_id" doc:"The Team's ID"`
	PrincipalID string `path:"principal_id" doc:"The member's principal ID"`
}

type SetTeamMemberRequest struct {
	TeamID      string `path:"team_id" doc:"The Team's ID"`
	PrincipalID string `path:"principal_id" doc:"The principal ID of an active user in the Organization"`
	Body        struct {
		Role string `json:"role" required:"true" enum:"lead,member" doc:"The member's role on the team"`
	}
}

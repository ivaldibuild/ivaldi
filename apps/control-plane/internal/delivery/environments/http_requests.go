package environments

import (
	"github.com/danielgtaylor/huma/v2"
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

// tierError reports a tier the domain does not know, at prefix.
func tierError(prefix *huma.PathBuffer, tier string) error {
	if _, err := delivery.Tier(tier).Valid(); err != nil {
		return &huma.ErrorDetail{Location: prefix.With("tier"), Message: err.Error(), Value: tier}
	}
	return nil
}

type CreateEnvironmentRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Environment belongs to"`
	Body        struct {
		DisplayName      string  `json:"display_name" required:"true" minLength:"3" doc:"The display name for the environment"`
		Tier             string  `json:"tier" required:"true" doc:"The environment tier for policy matching"`
		Slug             *string `json:"slug,omitempty" required:"false" doc:"Url slug for the environment; derived from the display name when omitted"`
		ExecutionEnabled bool    `json:"execution_enabled" required:"true" doc:"Whether agents may run work in the environment"`
	}
}

func (r *CreateEnvironmentRequest) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	if err := tierError(prefix, r.Body.Tier); err != nil {
		return []error{err}
	}
	return nil
}

func (r *CreateEnvironmentRequest) input(organizationID string) delivery.CreateEnvironmentInput {
	in := delivery.CreateEnvironmentInput{
		OrganizationID:   organizationID,
		WorkspaceID:      r.WorkspaceID,
		DisplayName:      r.Body.DisplayName,
		Tier:             delivery.Tier(r.Body.Tier),
		ExecutionEnabled: r.Body.ExecutionEnabled,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	return in
}

type ListEnvironmentsRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Environments in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Environments"`
	PageParams
}

type EnvironmentRequest struct {
	EnvironmentID string `path:"environment_id" doc:"The Environment's ID"`
}

type UpdateEnvironmentRequest struct {
	EnvironmentID string `path:"environment_id" doc:"The Environment's ID"`
	Body          struct {
		DisplayName      *string `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Tier             *string `json:"tier,omitempty" required:"false" doc:"New tier"`
		Slug             *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		ExecutionEnabled *bool   `json:"execution_enabled,omitempty" required:"false" doc:"Whether agents may run work in the environment"`
	}
}

func (r *UpdateEnvironmentRequest) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	if r.Body.Tier != nil {
		if err := tierError(prefix, *r.Body.Tier); err != nil {
			return []error{err}
		}
	}
	return nil
}

func (r *UpdateEnvironmentRequest) props() delivery.UpdateEnvironmentProps {
	p := delivery.UpdateEnvironmentProps{
		Slug:             r.Body.Slug,
		DisplayName:      r.Body.DisplayName,
		ExecutionEnabled: r.Body.ExecutionEnabled,
	}
	if r.Body.Tier != nil {
		t := delivery.Tier(*r.Body.Tier)
		p.Tier = &t
	}
	return p
}

type CreateTargetRequest struct {
	EnvironmentID string `path:"environment_id" doc:"The Environment the Target belongs to"`
	Body          struct {
		DisplayName      string            `json:"display_name" required:"true" minLength:"3" doc:"The display name for the target"`
		Slug             *string           `json:"slug,omitempty" required:"false" doc:"Url slug for the target; derived from the display name when omitted"`
		Runtime          string            `json:"runtime" required:"true" enum:"kubernetes" doc:"The runtime the target deploys to"`
		Locator          map[string]string `json:"locator" required:"true" doc:"Where the target is, in the runtime's terms"`
		AgentPrincipalID *string           `json:"agent_principal_id,omitempty" required:"false" doc:"The runtime agent that serves the target"`
	}
}

func (r *CreateTargetRequest) input(organizationID string) delivery.CreateTargetInput {
	in := delivery.CreateTargetInput{
		OrganizationID: organizationID,
		EnvironmentID:  r.EnvironmentID,
		DisplayName:    r.Body.DisplayName,
		Runtime:        delivery.Runtime(r.Body.Runtime),
		Locator:        r.Body.Locator,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	if r.Body.AgentPrincipalID != nil {
		in.AgentPrincipalID = *r.Body.AgentPrincipalID
	}
	return in
}

type ListTargetsRequest struct {
	EnvironmentID   string `path:"environment_id" doc:"The Environment to list Targets in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Targets"`
	PageParams
}

type TargetRequest struct {
	TargetID string `path:"target_id" doc:"The Target's ID"`
}

type UpdateTargetRequest struct {
	TargetID string `path:"target_id" doc:"The Target's ID"`
	Body     struct {
		DisplayName *string           `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Slug        *string           `json:"slug,omitempty" required:"false" doc:"New slug"`
		Locator     map[string]string `json:"locator,omitempty" required:"false" doc:"New locator, replacing the old one"`
	}
}

func (r *UpdateTargetRequest) props() delivery.UpdateTargetProps {
	return delivery.UpdateTargetProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Locator: r.Body.Locator}
}

type AssignTargetAgentRequest struct {
	TargetID string `path:"target_id" doc:"The Target's ID"`
	Body     struct {
		AgentPrincipalID string `json:"agent_principal_id" required:"true" doc:"The runtime agent's principal ID; empty unassigns the agent"`
	}
}

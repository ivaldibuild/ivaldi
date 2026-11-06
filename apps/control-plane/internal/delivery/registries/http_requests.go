package registries

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

type CreateRegistryRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Registry belongs to"`
	Body        struct {
		DisplayName   string  `json:"display_name" required:"true" minLength:"3" doc:"The display name for the registry"`
		Slug          *string `json:"slug,omitempty" required:"false" doc:"Url slug for the registry; derived from the display name when omitted"`
		Kind          string  `json:"kind" required:"true" enum:"oci" doc:"The kind of registry"`
		URL           string  `json:"url" required:"true" doc:"The registry's https or http address, without credentials"`
		CredentialRef *string `json:"credential_ref,omitempty" required:"false" doc:"The credential lookups use; anonymous when omitted"`
		LookupFrom    *string `json:"lookup_from,omitempty" required:"false" enum:"control_plane,data_plane" doc:"Where lookups run; control_plane when omitted"`
		LookupPool    *string `json:"lookup_pool,omitempty" required:"false" doc:"The data-plane pool that runs lookups, with lookup_from data_plane"`
	}
}

func (r *CreateRegistryRequest) input(organizationID string) delivery.CreateRegistryInput {
	in := delivery.CreateRegistryInput{
		OrganizationID: organizationID,
		WorkspaceID:    r.WorkspaceID,
		DisplayName:    r.Body.DisplayName,
		Kind:           delivery.RegistryKind(r.Body.Kind),
		URL:            r.Body.URL,
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	if r.Body.CredentialRef != nil {
		in.CredentialRef = *r.Body.CredentialRef
	}
	if r.Body.LookupFrom != nil {
		in.LookupFrom = delivery.LookupFrom(*r.Body.LookupFrom)
	}
	if r.Body.LookupPool != nil {
		in.LookupPool = *r.Body.LookupPool
	}
	return in
}

type ListRegistriesRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Registries in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Registries"`
	PageParams
}

type RegistryRequest struct {
	RegistryID string `path:"registry_id" doc:"The Registry's ID"`
}

type UpdateRegistryRequest struct {
	RegistryID string `path:"registry_id" doc:"The Registry's ID"`
	Body       struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		URL         *string `json:"url,omitempty" required:"false" doc:"New address"`
	}
}

func (r *UpdateRegistryRequest) props() delivery.UpdateRegistryProps {
	return delivery.UpdateRegistryProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, URL: r.Body.URL}
}

type SetRegistryCredentialRequest struct {
	RegistryID string `path:"registry_id" doc:"The Registry's ID"`
	Body       struct {
		CredentialRef string `json:"credential_ref" required:"true" doc:"The credential lookups use; empty makes the registry anonymous"`
	}
}

type SetRegistryLookupRequest struct {
	RegistryID string `path:"registry_id" doc:"The Registry's ID"`
	Body       struct {
		LookupFrom string  `json:"lookup_from" required:"true" enum:"control_plane,data_plane" doc:"Where lookups run"`
		LookupPool *string `json:"lookup_pool,omitempty" required:"false" doc:"The data-plane pool that runs lookups, with lookup_from data_plane"`
	}
}

func (r *SetRegistryLookupRequest) pool() string {
	if r.Body.LookupPool == nil {
		return ""
	}
	return *r.Body.LookupPool
}

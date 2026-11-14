package clusters

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

type CreateClusterRequest struct {
	Body struct {
		DisplayName string  `json:"display_name" required:"true" minLength:"3" maxLength:"255" doc:"The display name for the cluster"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"Url slug for the cluster; derived from the display name when omitted, and never changes"`
		Runtime     string  `json:"runtime" required:"true" enum:"kubernetes" doc:"The runtime the cluster runs"`
	}
}

func (r *CreateClusterRequest) input(organizationID string) delivery.CreateClusterInput {
	in := delivery.CreateClusterInput{
		OrganizationID: organizationID,
		DisplayName:    r.Body.DisplayName,
		Runtime:        delivery.Runtime(r.Body.Runtime),
	}
	if r.Body.Slug != nil {
		in.Slug = *r.Body.Slug
	}
	return in
}

type ListClustersRequest struct {
	IncludeArchived bool `query:"include_archived" doc:"Include archived Clusters"`
	PageParams
}

type ClusterRequest struct {
	ClusterID string `path:"cluster_id" doc:"The Cluster's ID"`
}

type UpdateClusterRequest struct {
	ClusterID string `path:"cluster_id" doc:"The Cluster's ID"`
	Body      struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"3" maxLength:"255" doc:"New display name. The slug and runtime do not change"`
	}
}

func (r *UpdateClusterRequest) props() delivery.UpdateClusterProps {
	return delivery.UpdateClusterProps{DisplayName: r.Body.DisplayName}
}

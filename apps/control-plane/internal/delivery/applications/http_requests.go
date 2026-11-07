package applications

import (
	"encoding/json"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Applications.

type CreateApplicationRequest struct {
	ProjectID string `path:"project_id" doc:"The Project the Application belongs to"`
	Body      struct {
		DisplayName        string            `json:"display_name" required:"true" minLength:"3" doc:"The display name for the application"`
		Slug               *string           `json:"slug,omitempty" required:"false" doc:"Url slug for the application; derived from the display name when omitted"`
		OwnerTeamID        *string           `json:"owner_team_id,omitempty" required:"false" doc:"The Team that owns the application; the Project's owning Team when omitted"`
		SourceID           *string           `json:"source_id,omitempty" required:"false" doc:"The Source the application is built from"`
		WorkloadSelector   map[string]string `json:"workload_selector,omitempty" required:"false" doc:"Labels that pick out the application's workloads; a label with the application's ID when omitted"`
		DefaultLifecycleID *string           `json:"default_lifecycle_id,omitempty" required:"false" doc:"The Lifecycle the default Channel follows, in the Project's Workspace; the delivery.default_lifecycle setting for the Project when omitted"`
	}
}

func (r *CreateApplicationRequest) input(organizationID string) delivery.CreateApplicationInput {
	return delivery.CreateApplicationInput{
		OrganizationID:     organizationID,
		ProjectID:          r.ProjectID,
		OwnerTeamID:        deref(r.Body.OwnerTeamID),
		Slug:               deref(r.Body.Slug),
		DisplayName:        r.Body.DisplayName,
		SourceID:           deref(r.Body.SourceID),
		WorkloadSelector:   r.Body.WorkloadSelector,
		DefaultLifecycleID: deref(r.Body.DefaultLifecycleID),
	}
}

// ApplicationFilter narrows an Application list.
type ApplicationFilter struct {
	OwnerTeamID     string `query:"owner_team_id" doc:"Only the Applications this Team owns"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Applications"`
	PageParams
}

type ListApplicationsRequest struct {
	ProjectID string `path:"project_id" doc:"The Project to list Applications in"`
	ApplicationFilter
}

type ListWorkspaceApplicationsRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace to list Applications in"`
	ApplicationFilter
}

type ApplicationRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
}

type UpdateApplicationRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		DisplayName      *string           `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Slug             *string           `json:"slug,omitempty" required:"false" doc:"New slug"`
		WorkloadSelector map[string]string `json:"workload_selector,omitempty" required:"false" doc:"New workload selector, replacing the old one"`
	}
}

func (r *UpdateApplicationRequest) props() delivery.UpdateApplicationProps {
	return delivery.UpdateApplicationProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, WorkloadSelector: r.Body.WorkloadSelector}
}

type LinkApplicationSourceRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		SourceID string `json:"source_id" required:"true" doc:"The Source the application is built from; empty removes it"`
	}
}

type TransferApplicationRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		OwnerTeamID string `json:"owner_team_id" required:"true" minLength:"1" doc:"The active Team to hand the application to"`
	}
}

// Artifacts.

type CreateArtifactRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		Name         string  `json:"name" required:"true" minLength:"1" doc:"The artifact's name in the application, such as api or chart"`
		ArtifactKind string  `json:"artifact_kind" required:"true" enum:"container_image,helm_chart,archive" doc:"What kind of artifact it is"`
		ArtifactName string  `json:"artifact_name" required:"true" minLength:"1" doc:"What its Builds are published under, without a tag or digest"`
		RegistryID   *string `json:"registry_id,omitempty" required:"false" doc:"The Registry it is looked up in, in the application's Workspace"`
		Required     bool    `json:"required,omitempty" required:"false" doc:"Whether every Release must include a Build of it"`
	}
}

func (r *CreateArtifactRequest) input(organizationID string) delivery.CreateArtifactInput {
	return delivery.CreateArtifactInput{
		OrganizationID: organizationID,
		ApplicationID:  r.ApplicationIdentifier,
		Name:           r.Body.Name,
		ArtifactKind:   delivery.ArtifactKind(r.Body.ArtifactKind),
		ArtifactName:   r.Body.ArtifactName,
		RegistryID:     deref(r.Body.RegistryID),
		Required:       r.Body.Required,
	}
}

type ListArtifactsRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	IncludeArchived       bool   `query:"include_archived" doc:"Include archived artifacts"`
}

type ArtifactRequest struct {
	ArtifactID string `path:"artifact_id" doc:"The artifact's ID"`
}

type UpdateArtifactRequest struct {
	ArtifactID string `path:"artifact_id" doc:"The artifact's ID"`
	Body       struct {
		Name         *string `json:"name,omitempty" required:"false" minLength:"1" doc:"New name"`
		ArtifactName *string `json:"artifact_name,omitempty" required:"false" minLength:"1" doc:"New published name"`
		Required     *bool   `json:"required,omitempty" required:"false" doc:"Whether every Release must include a Build of it"`
	}
}

func (r *UpdateArtifactRequest) props() delivery.UpdateArtifactProps {
	return delivery.UpdateArtifactProps{Name: r.Body.Name, ArtifactName: r.Body.ArtifactName, Required: r.Body.Required}
}

type SetArtifactRegistryRequest struct {
	ArtifactID string `path:"artifact_id" doc:"The artifact's ID"`
	Body       struct {
		RegistryID string `json:"registry_id" required:"true" doc:"The Registry it is looked up in; empty removes it"`
	}
}

// Channels.

// RuleBody is a Channel rule as a request gives it.
type RuleBody struct {
	Kind       string          `json:"kind" required:"true" enum:"version,git_ref" doc:"What the rule checks"`
	ArtifactID *string         `json:"artifact_id,omitempty" required:"false" doc:"The application's artifact the rule is scoped to"`
	Config     json.RawMessage `json:"config" required:"true" doc:"The rule's parameters"`
}

func (b RuleBody) props() delivery.ChannelRuleProps {
	return delivery.ChannelRuleProps{Kind: release.RuleKind(b.Kind), ArtifactID: b.ArtifactID, Config: b.Config}
}

type CreateChannelRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		DisplayName string     `json:"display_name" required:"true" minLength:"3" doc:"The display name for the channel"`
		Slug        *string    `json:"slug,omitempty" required:"false" doc:"Url slug for the channel; derived from the display name when omitted"`
		Description *string    `json:"description,omitempty" required:"false" doc:"What the channel is for"`
		LifecycleID string     `json:"lifecycle_id" required:"true" doc:"The Lifecycle the channel follows, in the application's Workspace"`
		Rules       []RuleBody `json:"rules,omitempty" required:"false" doc:"The rules a Release must meet"`
	}
}

func (r *CreateChannelRequest) input(organizationID string) delivery.CreateChannelInput {
	in := delivery.CreateChannelInput{
		OrganizationID: organizationID,
		ApplicationID:  r.ApplicationIdentifier,
		LifecycleID:    r.Body.LifecycleID,
		Slug:           deref(r.Body.Slug),
		DisplayName:    r.Body.DisplayName,
		Description:    deref(r.Body.Description),
	}
	for _, rule := range r.Body.Rules {
		in.Rules = append(in.Rules, rule.props())
	}
	return in
}

type ListChannelsRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	IncludeArchived       bool   `query:"include_archived" doc:"Include archived Channels"`
}

type ChannelRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
}

type UpdateChannelRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
	Body      struct {
		DisplayName *string `json:"display_name,omitempty" required:"false" minLength:"3" doc:"New display name"`
		Slug        *string `json:"slug,omitempty" required:"false" doc:"New slug"`
		Description *string `json:"description,omitempty" required:"false" doc:"New description; empty clears it"`
	}
}

func (r *UpdateChannelRequest) props() delivery.UpdateChannelProps {
	return delivery.UpdateChannelProps{Slug: r.Body.Slug, DisplayName: r.Body.DisplayName, Description: r.Body.Description}
}

type SetChannelLifecycleRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
	Body      struct {
		LifecycleID string `json:"lifecycle_id" required:"true" doc:"The Lifecycle the channel follows, in the application's Workspace"`
	}
}

type AddChannelRuleRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
	Body      RuleBody
}

type ChannelRuleRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
	RuleID    string `path:"rule_id" doc:"The rule's ID"`
}

type UpdateChannelRuleRequest struct {
	ChannelID string `path:"channel_id" doc:"The Channel's ID"`
	RuleID    string `path:"rule_id" doc:"The rule's ID"`
	Body      RuleBody
}

// Placements.

// ApplyConfigBody says how Ivaldi applies Releases to a Target.
type ApplyConfigBody struct {
	DefinitionArtifact string   `json:"definition_artifact" required:"true" doc:"The name of the application's artifact that holds the deployment definition"`
	ValuesRefs         []string `json:"values_refs,omitempty" required:"false" doc:"References to Target-specific values, never the values"`
}

func (b *ApplyConfigBody) config() *delivery.ApplyConfig {
	if b == nil {
		return nil
	}
	return &delivery.ApplyConfig{DefinitionArtifact: b.DefinitionArtifact, ValuesRefs: b.ValuesRefs}
}

type CreatePlacementRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		TargetID    string           `json:"target_id" required:"true" doc:"The Target to place it on, in the application's Workspace"`
		ApplyConfig *ApplyConfigBody `json:"apply_config,omitempty" required:"false" doc:"How Releases are applied there; Deployments are tracked only when omitted"`
	}
}

func (r *CreatePlacementRequest) input(organizationID string) delivery.CreatePlacementInput {
	return delivery.CreatePlacementInput{
		OrganizationID: organizationID,
		ApplicationID:  r.ApplicationIdentifier,
		TargetID:       r.Body.TargetID,
		ApplyConfig:    r.Body.ApplyConfig.config(),
	}
}

type ListPlacementsRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
}

type PlacementRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	TargetID              string `path:"target_id" doc:"The Target's ID"`
}

type ConfigurePlacementRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	TargetID              string `path:"target_id" doc:"The Target's ID"`
	Body                  struct {
		ApplyConfig *ApplyConfigBody `json:"apply_config,omitempty" required:"false" doc:"How Releases are applied there; omitting it makes Deployments there tracked only"`
	}
}

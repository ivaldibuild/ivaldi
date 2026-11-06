package applications

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// ApplicationController serves the operations on Applications, their
// artifacts, their Channels, and their placements. Every operation requires
// an access token; the caller's Organization comes from the token, never
// from the request.
type ApplicationController struct {
	service delivery.ApplicationService
}

var _ api.Routes = (*ApplicationController)(nil)

func NewApplicationController(service delivery.ApplicationService) *ApplicationController {
	return &ApplicationController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *ApplicationController) RegisterRoutes(h huma.API) {
	register(h, "create-application", http.MethodPost, "/projects/{project_id}/applications", "Create an Application in a Project, with its default Channel", http.StatusCreated, c.CreateApplication)
	register(h, "list-applications", http.MethodGet, "/projects/{project_id}/applications", "List a Project's Applications", 0, c.ListApplications)
	register(h, "list-workspace-applications", http.MethodGet, "/workspaces/{workspace_id}/applications", "List a Workspace's Applications", 0, c.ListWorkspaceApplications)
	register(h, "get-application", http.MethodGet, "/applications/{application_id}", "Get an Application", 0, c.GetApplication)
	register(h, "update-application", http.MethodPatch, "/applications/{application_id}", "Change an Application", 0, c.UpdateApplication)
	register(h, "link-application-source", http.MethodPut, "/applications/{application_id}/source", "Link or unlink the Source an Application is built from", 0, c.LinkApplicationSource)
	register(h, "transfer-application", http.MethodPost, "/applications/{application_id}/transfer", "Hand an Application to another Team", 0, c.TransferApplication)
	register(h, "archive-application", http.MethodPost, "/applications/{application_id}/archive", "Archive an Application", 0, c.ArchiveApplication)
	register(h, "restore-application", http.MethodPost, "/applications/{application_id}/restore", "Restore an archived Application", 0, c.RestoreApplication)

	register(h, "create-artifact", http.MethodPost, "/applications/{application_id}/artifacts", "Declare an artifact of an Application", http.StatusCreated, c.CreateArtifact)
	register(h, "list-artifacts", http.MethodGet, "/applications/{application_id}/artifacts", "List an Application's artifacts", 0, c.ListArtifacts)
	register(h, "update-artifact", http.MethodPatch, "/artifacts/{artifact_id}", "Change an artifact", 0, c.UpdateArtifact)
	register(h, "set-artifact-registry", http.MethodPut, "/artifacts/{artifact_id}/registry", "Set or clear the Registry an artifact is looked up in", 0, c.SetArtifactRegistry)
	register(h, "declare-artifact", http.MethodPost, "/artifacts/{artifact_id}/declare", "Confirm an artifact the runtime agent observed", 0, c.DeclareArtifact)
	register(h, "archive-artifact", http.MethodPost, "/artifacts/{artifact_id}/archive", "Archive an artifact", 0, c.ArchiveArtifact)
	register(h, "restore-artifact", http.MethodPost, "/artifacts/{artifact_id}/restore", "Restore an archived artifact", 0, c.RestoreArtifact)

	register(h, "create-channel", http.MethodPost, "/applications/{application_id}/channels", "Add a Channel to an Application", http.StatusCreated, c.CreateChannel)
	register(h, "list-channels", http.MethodGet, "/applications/{application_id}/channels", "List an Application's Channels", 0, c.ListChannels)
	register(h, "get-channel", http.MethodGet, "/channels/{channel_id}", "Get a Channel", 0, c.GetChannel)
	register(h, "update-channel", http.MethodPatch, "/channels/{channel_id}", "Change a Channel", 0, c.UpdateChannel)
	register(h, "set-channel-lifecycle", http.MethodPut, "/channels/{channel_id}/lifecycle", "Set the Lifecycle a Channel follows", 0, c.SetChannelLifecycle)
	register(h, "set-default-channel", http.MethodPost, "/channels/{channel_id}/default", "Make a Channel its Application's default", 0, c.SetDefaultChannel)
	register(h, "archive-channel", http.MethodPost, "/channels/{channel_id}/archive", "Archive a Channel that is not the default", 0, c.ArchiveChannel)
	register(h, "restore-channel", http.MethodPost, "/channels/{channel_id}/restore", "Restore an archived Channel", 0, c.RestoreChannel)
	register(h, "add-channel-rule", http.MethodPost, "/channels/{channel_id}/rules", "Add a rule to a Channel", 0, c.AddChannelRule)
	register(h, "update-channel-rule", http.MethodPut, "/channels/{channel_id}/rules/{rule_id}", "Replace a Channel rule", 0, c.UpdateChannelRule)
	register(h, "remove-channel-rule", http.MethodDelete, "/channels/{channel_id}/rules/{rule_id}", "Remove a Channel rule", 0, c.RemoveChannelRule)

	register(h, "create-placement", http.MethodPost, "/applications/{application_id}/placements", "Place an Application on a Target", http.StatusCreated, c.CreatePlacement)
	register(h, "list-placements", http.MethodGet, "/applications/{application_id}/placements", "List the Targets an Application is placed on", 0, c.ListPlacements)
	register(h, "configure-placement", http.MethodPut, "/applications/{application_id}/placements/{target_id}", "Set or clear how Releases are applied to a Target", 0, c.ConfigurePlacement)
	register(h, "remove-placement", http.MethodDelete, "/applications/{application_id}/placements/{target_id}", "Remove an Application from a Target", http.StatusNoContent, c.RemovePlacement)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"applications"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

// caller returns the authenticated caller. The API refuses a secured
// operation without a valid token, so a missing caller is a wiring bug.
func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

// call runs an operation as the caller and maps its result to a response.
func call[T, R any](ctx context.Context, op func(identity.Caller) (T, error), respond func(T) R) (R, error) {
	var zero R
	who, err := caller(ctx)
	if err != nil {
		return zero, err
	}
	v, err := op(who)
	if err != nil {
		return zero, httpError(ctx, err)
	}
	return respond(v), nil
}

// Applications.

func (c *ApplicationController) CreateApplication(ctx context.Context, req *CreateApplicationRequest) (*CreateApplicationResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	app, ch, err := c.service.CreateApplication(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return &CreateApplicationResponse{Body: api.SuccessResponse(nil, CreatedApplication{Application: app.Snapshot(), DefaultChannel: ch.Snapshot()})}, nil
}

func (c *ApplicationController) ListApplications(ctx context.Context, req *ListApplicationsRequest) (*ApplicationListResponse, error) {
	return c.list(ctx, delivery.ListApplicationsQuery{ProjectID: req.ProjectID}, req.ApplicationFilter)
}

func (c *ApplicationController) ListWorkspaceApplications(ctx context.Context, req *ListWorkspaceApplicationsRequest) (*ApplicationListResponse, error) {
	return c.list(ctx, delivery.ListApplicationsQuery{WorkspaceID: req.WorkspaceID}, req.ApplicationFilter)
}

func (c *ApplicationController) list(ctx context.Context, q delivery.ListApplicationsQuery, f ApplicationFilter) (*ApplicationListResponse, error) {
	return call(ctx, func(who identity.Caller) (*pagination.Result[*delivery.Application], error) {
		q.OrganizationID = who.OrganizationID
		q.OwnerTeamID = f.OwnerTeamID
		q.IncludeArchived = f.IncludeArchived
		q.Page = f.query()
		return c.service.ListApplications(ctx, who.Actor, q)
	}, func(page *pagination.Result[*delivery.Application]) *ApplicationListResponse {
		return &ApplicationListResponse{Body: api.SuccessResponse(nil, snapshots(page, (*delivery.Application).Snapshot))}
	})
}

func (c *ApplicationController) GetApplication(ctx context.Context, req *ApplicationRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, c.service.GetApplication)
}

func (c *ApplicationController) UpdateApplication(ctx context.Context, req *UpdateApplicationRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, func(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
		return c.service.UpdateApplication(ctx, actor, organizationID, applicationID, req.props())
	})
}

func (c *ApplicationController) LinkApplicationSource(ctx context.Context, req *LinkApplicationSourceRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, func(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
		return c.service.LinkApplicationSource(ctx, actor, organizationID, applicationID, req.Body.SourceID)
	})
}

func (c *ApplicationController) TransferApplication(ctx context.Context, req *TransferApplicationRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, func(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
		return c.service.TransferApplication(ctx, actor, organizationID, applicationID, req.Body.OwnerTeamID)
	})
}

func (c *ApplicationController) ArchiveApplication(ctx context.Context, req *ApplicationRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, c.service.ArchiveApplication)
}

func (c *ApplicationController) RestoreApplication(ctx context.Context, req *ApplicationRequest) (*ApplicationResponse, error) {
	return c.application(ctx, req.ApplicationID, c.service.RestoreApplication)
}

// application calls an operation on one Application.
func (c *ApplicationController) application(ctx context.Context, applicationID string, op func(context.Context, identity.Actor, string, string) (*delivery.Application, error)) (*ApplicationResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Application, error) {
		return op(ctx, who.Actor, who.OrganizationID, applicationID)
	}, applicationResponse)
}

// Artifacts.

func (c *ApplicationController) CreateArtifact(ctx context.Context, req *CreateArtifactRequest) (*ArtifactResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Artifact, error) {
		return c.service.CreateArtifact(ctx, who.Actor, req.input(who.OrganizationID))
	}, artifactResponse)
}

func (c *ApplicationController) ListArtifacts(ctx context.Context, req *ListArtifactsRequest) (*ArtifactListResponse, error) {
	return call(ctx, func(who identity.Caller) ([]*delivery.Artifact, error) {
		return c.service.ListArtifacts(ctx, who.Actor, who.OrganizationID, req.ApplicationID, req.IncludeArchived)
	}, func(items []*delivery.Artifact) *ArtifactListResponse {
		return &ArtifactListResponse{Body: api.SuccessResponse(nil, snapshotAll(items, (*delivery.Artifact).Snapshot))}
	})
}

func (c *ApplicationController) UpdateArtifact(ctx context.Context, req *UpdateArtifactRequest) (*ArtifactResponse, error) {
	return c.artifact(ctx, req.ArtifactID, func(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
		return c.service.UpdateArtifact(ctx, actor, organizationID, artifactID, req.props())
	})
}

func (c *ApplicationController) SetArtifactRegistry(ctx context.Context, req *SetArtifactRegistryRequest) (*ArtifactResponse, error) {
	return c.artifact(ctx, req.ArtifactID, func(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
		return c.service.SetArtifactRegistry(ctx, actor, organizationID, artifactID, req.Body.RegistryID)
	})
}

func (c *ApplicationController) DeclareArtifact(ctx context.Context, req *ArtifactRequest) (*ArtifactResponse, error) {
	return c.artifact(ctx, req.ArtifactID, c.service.DeclareArtifact)
}

func (c *ApplicationController) ArchiveArtifact(ctx context.Context, req *ArtifactRequest) (*ArtifactResponse, error) {
	return c.artifact(ctx, req.ArtifactID, c.service.ArchiveArtifact)
}

func (c *ApplicationController) RestoreArtifact(ctx context.Context, req *ArtifactRequest) (*ArtifactResponse, error) {
	return c.artifact(ctx, req.ArtifactID, c.service.RestoreArtifact)
}

// artifact calls an operation on one artifact.
func (c *ApplicationController) artifact(ctx context.Context, artifactID string, op func(context.Context, identity.Actor, string, string) (*delivery.Artifact, error)) (*ArtifactResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Artifact, error) {
		return op(ctx, who.Actor, who.OrganizationID, artifactID)
	}, artifactResponse)
}

// Channels.

func (c *ApplicationController) CreateChannel(ctx context.Context, req *CreateChannelRequest) (*ChannelResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Channel, error) {
		return c.service.CreateChannel(ctx, who.Actor, req.input(who.OrganizationID))
	}, channelResponse)
}

func (c *ApplicationController) ListChannels(ctx context.Context, req *ListChannelsRequest) (*ChannelListResponse, error) {
	return call(ctx, func(who identity.Caller) ([]*delivery.Channel, error) {
		return c.service.ListChannels(ctx, who.Actor, who.OrganizationID, req.ApplicationID, req.IncludeArchived)
	}, func(items []*delivery.Channel) *ChannelListResponse {
		return &ChannelListResponse{Body: api.SuccessResponse(nil, snapshotAll(items, (*delivery.Channel).Snapshot))}
	})
}

func (c *ApplicationController) GetChannel(ctx context.Context, req *ChannelRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, c.service.GetChannel)
}

func (c *ApplicationController) UpdateChannel(ctx context.Context, req *UpdateChannelRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, func(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
		return c.service.UpdateChannel(ctx, actor, organizationID, channelID, req.props())
	})
}

func (c *ApplicationController) SetChannelLifecycle(ctx context.Context, req *SetChannelLifecycleRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, func(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
		return c.service.SetChannelLifecycle(ctx, actor, organizationID, channelID, req.Body.LifecycleID)
	})
}

func (c *ApplicationController) SetDefaultChannel(ctx context.Context, req *ChannelRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, c.service.SetDefaultChannel)
}

func (c *ApplicationController) ArchiveChannel(ctx context.Context, req *ChannelRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, c.service.ArchiveChannel)
}

func (c *ApplicationController) RestoreChannel(ctx context.Context, req *ChannelRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, c.service.RestoreChannel)
}

func (c *ApplicationController) AddChannelRule(ctx context.Context, req *AddChannelRuleRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, func(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
		return c.service.AddChannelRule(ctx, actor, organizationID, channelID, req.Body.props())
	})
}

func (c *ApplicationController) UpdateChannelRule(ctx context.Context, req *UpdateChannelRuleRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, func(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
		return c.service.UpdateChannelRule(ctx, actor, organizationID, channelID, req.RuleID, req.Body.props())
	})
}

func (c *ApplicationController) RemoveChannelRule(ctx context.Context, req *ChannelRuleRequest) (*ChannelResponse, error) {
	return c.channel(ctx, req.ChannelID, func(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
		return c.service.RemoveChannelRule(ctx, actor, organizationID, channelID, req.RuleID)
	})
}

// channel calls an operation on one Channel.
func (c *ApplicationController) channel(ctx context.Context, channelID string, op func(context.Context, identity.Actor, string, string) (*delivery.Channel, error)) (*ChannelResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Channel, error) {
		return op(ctx, who.Actor, who.OrganizationID, channelID)
	}, channelResponse)
}

// Placements.

func (c *ApplicationController) CreatePlacement(ctx context.Context, req *CreatePlacementRequest) (*PlacementResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Placement, error) {
		return c.service.CreatePlacement(ctx, who.Actor, req.input(who.OrganizationID))
	}, placementResponse)
}

func (c *ApplicationController) ListPlacements(ctx context.Context, req *ListPlacementsRequest) (*PlacementListResponse, error) {
	return call(ctx, func(who identity.Caller) ([]*delivery.Placement, error) {
		return c.service.ListPlacements(ctx, who.Actor, who.OrganizationID, req.ApplicationID)
	}, func(items []*delivery.Placement) *PlacementListResponse {
		return &PlacementListResponse{Body: api.SuccessResponse(nil, snapshotAll(items, (*delivery.Placement).Snapshot))}
	})
}

func (c *ApplicationController) ConfigurePlacement(ctx context.Context, req *ConfigurePlacementRequest) (*PlacementResponse, error) {
	return call(ctx, func(who identity.Caller) (*delivery.Placement, error) {
		return c.service.ConfigurePlacement(ctx, who.Actor, who.OrganizationID, req.ApplicationID, req.TargetID, req.Body.ApplyConfig.config())
	}, placementResponse)
}

func (c *ApplicationController) RemovePlacement(ctx context.Context, req *PlacementRequest) (*struct{}, error) {
	return call(ctx, func(who identity.Caller) (struct{}, error) {
		return struct{}{}, c.service.RemovePlacement(ctx, who.Actor, who.OrganizationID, req.ApplicationID, req.TargetID)
	}, func(struct{}) *struct{} { return nil })
}

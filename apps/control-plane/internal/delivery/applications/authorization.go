package applications

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Applications are not IAM resources: every operation is checked on the
// Application's Project, and so are the operations on its artifacts,
// Channels, and placements. Listing a Workspace's Applications is checked on
// the Workspace. Finding the Project means reading the Application, and for
// an artifact or a Channel reading it first; next reads them again in its
// transaction, so a decision is never applied to stale state. None of them
// moves to another Application or Project.
//
// An Application may be named by its ID or by its slug. The wrapper resolves
// it once, decides on that Application, and passes its ID to next, so a slug
// that comes to name another Application meanwhile cannot redirect the change.
type authorized struct {
	next         delivery.ApplicationService
	authz        authorization.Authorizer
	applications delivery.ApplicationRepository
}

var _ delivery.ApplicationService = (*authorized)(nil)

// allow asks for a decision and returns ctx carrying it for the call.
func (a *authorized) allow(ctx context.Context, req authorization.Request) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, req)
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

// onApplication checks a permission on the Project of the Application an ID
// or slug names, and returns the Application's ID for next.
func (a *authorized) onApplication(ctx context.Context, actor identity.Actor, permission, action, organizationID, application string, proposal any) (context.Context, string, error) {
	app, err := a.applications.GetApplicationByIdentifier(ctx, organizationID, application)
	if err != nil {
		return nil, "", err
	}
	ctx, err = a.allow(ctx, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: app.ProjectID(), Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, "", err
	}
	return ctx, app.ID(), nil
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, action, organizationID, application string, proposal any) (context.Context, string, error) {
	return a.onApplication(ctx, actor, delivery.PermissionApplicationManage, action, organizationID, application, proposal)
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, action, organizationID, application string) (string, error) {
	_, id, err := a.onApplication(ctx, actor, delivery.PermissionApplicationRead, action, organizationID, application, nil)
	return id, err
}

// manageArtifact checks a change to an artifact on its Application's Project.
func (a *authorized) manageArtifact(ctx context.Context, actor identity.Actor, action, organizationID, artifactID string, proposal any) (context.Context, error) {
	art, err := a.applications.GetArtifact(ctx, organizationID, artifactID)
	if err != nil {
		return nil, err
	}
	ctx, _, err = a.manage(ctx, actor, action, organizationID, art.ApplicationID(), proposal)
	return ctx, err
}

// manageChannel checks a change to a Channel on its Application's Project.
func (a *authorized) manageChannel(ctx context.Context, actor identity.Actor, action, organizationID, channelID string, proposal any) (context.Context, error) {
	c, err := a.applications.GetChannel(ctx, organizationID, channelID)
	if err != nil {
		return nil, err
	}
	ctx, _, err = a.manage(ctx, actor, action, organizationID, c.ApplicationID(), proposal)
	return ctx, err
}

// CreateApplication checks the input before asking, as nothing about it
// depends on stored state.
func (a *authorized) CreateApplication(ctx context.Context, actor identity.Actor, in delivery.CreateApplicationInput) (*delivery.Application, *delivery.Channel, error) {
	if err := in.Validate(); err != nil {
		return nil, nil, err
	}
	ctx, err := a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionApplicationManage, OrganizationID: in.OrganizationID,
		ResourceID: in.ProjectID, Action: delivery.ActionApplicationCreate, Proposal: in,
	})
	if err != nil {
		return nil, nil, err
	}
	return a.next.CreateApplication(ctx, actor, in)
}

func (a *authorized) GetApplication(ctx context.Context, actor identity.Actor, organizationID, application string) (*delivery.Application, error) {
	id, err := a.read(ctx, actor, delivery.ActionApplicationRead, organizationID, application)
	if err != nil {
		return nil, err
	}
	return a.next.GetApplication(ctx, actor, organizationID, id)
}

// ListApplications is checked on the Project when one is given, or else on
// the Workspace.
func (a *authorized) ListApplications(ctx context.Context, actor identity.Actor, query delivery.ListApplicationsQuery) (*pagination.Result[*delivery.Application], error) {
	resource := query.ProjectID
	if resource == "" {
		resource = query.WorkspaceID
	}
	if resource == "" {
		return nil, delivery.NewErrInvalidInput("listing applications needs a project or a workspace")
	}
	if _, err := a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionApplicationRead, OrganizationID: query.OrganizationID,
		ResourceID: resource, Action: delivery.ActionApplicationList,
	}); err != nil {
		return nil, err
	}
	return a.next.ListApplications(ctx, actor, query)
}

func (a *authorized) UpdateApplication(ctx context.Context, actor identity.Actor, organizationID, application string, props delivery.UpdateApplicationProps) (*delivery.Application, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionApplicationUpdate, organizationID, application, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateApplication(ctx, actor, organizationID, id, props)
}

func (a *authorized) LinkApplicationSource(ctx context.Context, actor identity.Actor, organizationID, application, sourceID string) (*delivery.Application, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionApplicationLinkSource, organizationID, application, sourceID)
	if err != nil {
		return nil, err
	}
	return a.next.LinkApplicationSource(ctx, actor, organizationID, id, sourceID)
}

func (a *authorized) TransferApplication(ctx context.Context, actor identity.Actor, organizationID, application, ownerTeamID string) (*delivery.Application, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionApplicationTransfer, organizationID, application, ownerTeamID)
	if err != nil {
		return nil, err
	}
	return a.next.TransferApplication(ctx, actor, organizationID, id, ownerTeamID)
}

func (a *authorized) ArchiveApplication(ctx context.Context, actor identity.Actor, organizationID, application string) (*delivery.Application, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionApplicationArchive, organizationID, application, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveApplication(ctx, actor, organizationID, id)
}

func (a *authorized) RestoreApplication(ctx context.Context, actor identity.Actor, organizationID, application string) (*delivery.Application, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionApplicationRestore, organizationID, application, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreApplication(ctx, actor, organizationID, id)
}

func (a *authorized) CreateArtifact(ctx context.Context, actor identity.Actor, in delivery.CreateArtifactInput) (*delivery.Artifact, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionArtifactCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}
	in.ApplicationID = id
	return a.next.CreateArtifact(ctx, actor, in)
}

func (a *authorized) ListArtifacts(ctx context.Context, actor identity.Actor, organizationID, application string, includeArchived bool) ([]*delivery.Artifact, error) {
	id, err := a.read(ctx, actor, delivery.ActionArtifactList, organizationID, application)
	if err != nil {
		return nil, err
	}
	return a.next.ListArtifacts(ctx, actor, organizationID, id, includeArchived)
}

func (a *authorized) UpdateArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string, props delivery.UpdateArtifactProps) (*delivery.Artifact, error) {
	ctx, err := a.manageArtifact(ctx, actor, delivery.ActionArtifactUpdate, organizationID, artifactID, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateArtifact(ctx, actor, organizationID, artifactID, props)
}

func (a *authorized) SetArtifactRegistry(ctx context.Context, actor identity.Actor, organizationID, artifactID, registryID string) (*delivery.Artifact, error) {
	ctx, err := a.manageArtifact(ctx, actor, delivery.ActionArtifactSetRegistry, organizationID, artifactID, registryID)
	if err != nil {
		return nil, err
	}
	return a.next.SetArtifactRegistry(ctx, actor, organizationID, artifactID, registryID)
}

func (a *authorized) DeclareArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	ctx, err := a.manageArtifact(ctx, actor, delivery.ActionArtifactDeclare, organizationID, artifactID, nil)
	if err != nil {
		return nil, err
	}
	return a.next.DeclareArtifact(ctx, actor, organizationID, artifactID)
}

func (a *authorized) ArchiveArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	ctx, err := a.manageArtifact(ctx, actor, delivery.ActionArtifactArchive, organizationID, artifactID, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveArtifact(ctx, actor, organizationID, artifactID)
}

func (a *authorized) RestoreArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*delivery.Artifact, error) {
	ctx, err := a.manageArtifact(ctx, actor, delivery.ActionArtifactRestore, organizationID, artifactID, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreArtifact(ctx, actor, organizationID, artifactID)
}

func (a *authorized) CreateChannel(ctx context.Context, actor identity.Actor, in delivery.CreateChannelInput) (*delivery.Channel, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionChannelCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}
	in.ApplicationID = id
	return a.next.CreateChannel(ctx, actor, in)
}

func (a *authorized) GetChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	c, err := a.next.GetChannel(ctx, actor, organizationID, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := a.read(ctx, actor, delivery.ActionChannelRead, organizationID, c.ApplicationID()); err != nil {
		return nil, err
	}
	return c, nil
}

func (a *authorized) ListChannels(ctx context.Context, actor identity.Actor, organizationID, application string, includeArchived bool) ([]*delivery.Channel, error) {
	id, err := a.read(ctx, actor, delivery.ActionChannelList, organizationID, application)
	if err != nil {
		return nil, err
	}
	return a.next.ListChannels(ctx, actor, organizationID, id, includeArchived)
}

func (a *authorized) UpdateChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string, props delivery.UpdateChannelProps) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelUpdate, organizationID, channelID, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateChannel(ctx, actor, organizationID, channelID, props)
}

func (a *authorized) SetChannelLifecycle(ctx context.Context, actor identity.Actor, organizationID, channelID, lifecycleID string) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelSetLifecycle, organizationID, channelID, lifecycleID)
	if err != nil {
		return nil, err
	}
	return a.next.SetChannelLifecycle(ctx, actor, organizationID, channelID, lifecycleID)
}

func (a *authorized) AddChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID string, props delivery.ChannelRuleProps) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelAddRule, organizationID, channelID, props)
	if err != nil {
		return nil, err
	}
	return a.next.AddChannelRule(ctx, actor, organizationID, channelID, props)
}

func (a *authorized) UpdateChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string, props delivery.ChannelRuleProps) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelUpdateRule, organizationID, channelID, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateChannelRule(ctx, actor, organizationID, channelID, ruleID, props)
}

func (a *authorized) RemoveChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelRemoveRule, organizationID, channelID, ruleID)
	if err != nil {
		return nil, err
	}
	return a.next.RemoveChannelRule(ctx, actor, organizationID, channelID, ruleID)
}

func (a *authorized) SetDefaultChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelSetDefault, organizationID, channelID, channelID)
	if err != nil {
		return nil, err
	}
	return a.next.SetDefaultChannel(ctx, actor, organizationID, channelID)
}

func (a *authorized) ArchiveChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelArchive, organizationID, channelID, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveChannel(ctx, actor, organizationID, channelID)
}

func (a *authorized) RestoreChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	ctx, err := a.manageChannel(ctx, actor, delivery.ActionChannelRestore, organizationID, channelID, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreChannel(ctx, actor, organizationID, channelID)
}

func (a *authorized) CreatePlacement(ctx context.Context, actor identity.Actor, in delivery.CreatePlacementInput) (*delivery.Placement, error) {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionPlacementCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}
	in.ApplicationID = id
	return a.next.CreatePlacement(ctx, actor, in)
}

func (a *authorized) ListPlacements(ctx context.Context, actor identity.Actor, organizationID, application string) ([]*delivery.Placement, error) {
	id, err := a.read(ctx, actor, delivery.ActionPlacementList, organizationID, application)
	if err != nil {
		return nil, err
	}
	return a.next.ListPlacements(ctx, actor, organizationID, id)
}

// ConfigurePlacement checks the config before asking, as nothing about it
// depends on stored state.
func (a *authorized) ConfigurePlacement(ctx context.Context, actor identity.Actor, organizationID, application, targetID string, config *delivery.ApplyConfig) (*delivery.Placement, error) {
	if config != nil {
		if err := config.Validate(); err != nil {
			return nil, err
		}
	}
	ctx, id, err := a.manage(ctx, actor, delivery.ActionPlacementConfigure, organizationID, application, config)
	if err != nil {
		return nil, err
	}
	return a.next.ConfigurePlacement(ctx, actor, organizationID, id, targetID, config)
}

func (a *authorized) RemovePlacement(ctx context.Context, actor identity.Actor, organizationID, application, targetID string) error {
	ctx, id, err := a.manage(ctx, actor, delivery.ActionPlacementRemove, organizationID, application, targetID)
	if err != nil {
		return err
	}
	return a.next.RemovePlacement(ctx, actor, organizationID, id, targetID)
}

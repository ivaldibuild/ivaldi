package sources

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Creating a Source and listing a Project's Sources are checked on the
// Project; reading, archiving, and listing by Workspace follow the resource
// itself. Restoring an archived Source is checked on its Project, because
// grants on an archived Source do not apply.
type authorized struct {
	next    versioncontrol.SourceService
	authz   authorization.Authorizer
	sources versioncontrol.SourceRepository
}

var _ versioncontrol.SourceService = (*authorized)(nil)

func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, organizationID, resourceID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: resourceID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string) error {
	_, err := a.decide(ctx, actor, versioncontrol.PermissionSourceRead, organizationID, resourceID, action, nil)
	return err
}

func (a *authorized) CreateSource(ctx context.Context, actor identity.Actor, in versioncontrol.CreateSourceInput) (*versioncontrol.Source, error) {
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionSourceManage, in.OrganizationID, in.ProjectID, versioncontrol.ActionSourceCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateSource(ctx, actor, in)
}

func (a *authorized) GetSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	if err := a.read(ctx, actor, organizationID, sourceID, versioncontrol.ActionSourceRead); err != nil {
		return nil, err
	}
	return a.next.GetSource(ctx, actor, organizationID, sourceID)
}

func (a *authorized) ListSources(ctx context.Context, actor identity.Actor, query versioncontrol.ListSourcesQuery) (*pagination.Result[*versioncontrol.Source], error) {
	resource := query.ProjectID
	if resource == "" {
		resource = query.WorkspaceID
	}
	if resource == "" {
		return nil, versioncontrol.NewErrInvalidInput("listing sources needs a project or a workspace")
	}
	if err := a.read(ctx, actor, query.OrganizationID, resource, versioncontrol.ActionSourceList); err != nil {
		return nil, err
	}
	return a.next.ListSources(ctx, actor, query)
}

func (a *authorized) ArchiveSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	ctx, err := a.decide(ctx, actor, versioncontrol.PermissionSourceManage, organizationID, sourceID, versioncontrol.ActionSourceArchive, sourceID)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveSource(ctx, actor, organizationID, sourceID)
}

func (a *authorized) RestoreSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	source, err := a.sources.GetSource(ctx, organizationID, sourceID)
	if err != nil {
		return nil, err
	}
	ctx, err = a.decide(ctx, actor, versioncontrol.PermissionSourceManage, organizationID, source.ProjectID(), versioncontrol.ActionSourceRestore, sourceID)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreSource(ctx, actor, organizationID, sourceID)
}

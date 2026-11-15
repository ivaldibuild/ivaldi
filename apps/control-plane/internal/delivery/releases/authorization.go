package releases

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
// Releases follow their Application, which is checked on its Project:
// reading needs application.read, recording Builds, creating, and
// withdrawing need release.manage, and publishing needs release.publish.
// Finding the Project means reading the Application, and for a Release
// reading it first; next reads them again in its transaction. An
// Application named by its slug is resolved once here and next gets its ID,
// so the decision and the change are about the same Application. The
// authorizer refuses agent Sessions, which may not publish without a
// human's approval (ADR 0007).
type authorized struct {
	next         delivery.ReleaseService
	authz        authorization.Authorizer
	applications Applications
	releases     delivery.ReleaseRepository
}

var _ delivery.ReleaseService = (*authorized)(nil)

// decide asks about permission on the Project of the Application an ID or
// slug names, and returns the Application's ID.
func (a *authorized) decide(ctx context.Context, actor identity.Actor, permission, action, organizationID, application string, proposal any) (authorization.Decision, string, error) {
	app, err := a.applications.GetApplicationByIdentifier(ctx, organizationID, application)
	if err != nil {
		return authorization.Decision{}, "", err
	}
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: permission, OrganizationID: organizationID,
		ResourceID: app.ProjectID(), Action: action, Proposal: proposal,
	})
	return d, app.ID(), err
}

func (a *authorized) allow(ctx context.Context, actor identity.Actor, permission, action, organizationID, application string, proposal any) (context.Context, string, error) {
	d, id, err := a.decide(ctx, actor, permission, action, organizationID, application, proposal)
	if err != nil {
		return nil, "", err
	}
	return authorization.WithDecision(ctx, "", d), id, nil
}

// onRelease asks about permission on a Release's Application.
func (a *authorized) onRelease(ctx context.Context, actor identity.Actor, permission, action, organizationID, releaseID string) (context.Context, error) {
	rel, err := a.releases.GetRelease(ctx, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	ctx, _, err = a.allow(ctx, actor, permission, action, organizationID, rel.ApplicationID(), rel.Snapshot())
	return ctx, err
}

func (a *authorized) ReportBuild(ctx context.Context, actor identity.Actor, in delivery.ReportBuildInput) (*delivery.Build, error) {
	ctx, id, err := a.allow(ctx, actor, delivery.PermissionReleaseManage, delivery.ActionBuildReport, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}
	in.ApplicationID = id
	return a.next.ReportBuild(ctx, actor, in)
}

// CreateRelease also asks to publish when the call publishes, and files
// that decision apart, so the published Release records it.
func (a *authorized) CreateRelease(ctx context.Context, actor identity.Actor, in delivery.CreateReleaseInput) (*delivery.ReleaseDetail, error) {
	ctx, id, err := a.allow(ctx, actor, delivery.PermissionReleaseManage, delivery.ActionReleaseCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}
	in.ApplicationID = id
	if in.Publish {
		d, _, err := a.decide(ctx, actor, delivery.PermissionReleasePublish, delivery.ActionReleasePublish, in.OrganizationID, id, in)
		if err != nil {
			return nil, err
		}
		ctx = authorization.WithDecision(ctx, publishSubject, d)
	}
	return a.next.CreateRelease(ctx, actor, in)
}

func (a *authorized) GetRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	if _, err := a.onRelease(ctx, actor, delivery.PermissionApplicationRead, delivery.ActionReleaseRead, organizationID, releaseID); err != nil {
		return nil, err
	}
	return a.next.GetRelease(ctx, actor, organizationID, releaseID)
}

func (a *authorized) GetReleaseVariables(ctx context.Context, actor identity.Actor, organizationID, releaseID, targetID string) (*delivery.ReleaseVariablesView, error) {
	if _, err := a.onRelease(ctx, actor, delivery.PermissionVariableRead, delivery.ActionReleaseVariablesRead, organizationID, releaseID); err != nil {
		return nil, err
	}
	return a.next.GetReleaseVariables(ctx, actor, organizationID, releaseID, targetID)
}

func (a *authorized) RefreshReleaseVariables(ctx context.Context, actor identity.Actor, organizationID, releaseID string, preview bool) (*delivery.ReleaseVariablesRefresh, error) {
	ctx, err := a.onRelease(ctx, actor, delivery.PermissionReleaseManage, delivery.ActionReleaseRefreshVariables, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	return a.next.RefreshReleaseVariables(ctx, actor, organizationID, releaseID, preview)
}

func (a *authorized) ListReleases(ctx context.Context, actor identity.Actor, q delivery.ListReleasesQuery) (*pagination.Result[*delivery.ReleaseDetail], error) {
	_, id, err := a.decide(ctx, actor, delivery.PermissionApplicationRead, delivery.ActionReleaseList, q.OrganizationID, q.ApplicationID, nil)
	if err != nil {
		return nil, err
	}
	q.ApplicationID = id
	return a.next.ListReleases(ctx, actor, q)
}

func (a *authorized) PublishRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	ctx, err := a.onRelease(ctx, actor, delivery.PermissionReleasePublish, delivery.ActionReleasePublish, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	return a.next.PublishRelease(ctx, actor, organizationID, releaseID)
}

func (a *authorized) WithdrawRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	ctx, err := a.onRelease(ctx, actor, delivery.PermissionReleaseManage, delivery.ActionReleaseWithdraw, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	return a.next.WithdrawRelease(ctx, actor, organizationID, releaseID)
}

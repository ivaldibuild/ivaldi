package lifecycles

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
// Lifecycles are not IAM resources: every operation is checked on the
// Lifecycle's Workspace. Finding it for an existing Lifecycle means reading
// the Lifecycle first; next reads it again, locked, in its transaction, so a
// decision is never applied to stale state. A Lifecycle never moves between
// Workspaces, so the Workspace read here is the one the change applies in.
type authorized struct {
	next       delivery.LifecycleService
	authz      authorization.Authorizer
	lifecycles delivery.LifecycleRepository
}

var _ delivery.LifecycleService = (*authorized)(nil)

// allow asks for a decision and returns ctx carrying it for the call.
func (a *authorized) allow(ctx context.Context, req authorization.Request) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, req)
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, workspaceID, action string) error {
	_, err := a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionLifecycleRead, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: action,
	})
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, workspaceID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, authorization.Request{
		Actor: actor, Permission: delivery.PermissionLifecycleManage, OrganizationID: organizationID,
		ResourceID: workspaceID, Action: action, Proposal: proposal,
	})
}

// manageLifecycle authorizes a change to a Lifecycle on its Workspace.
func (a *authorized) manageLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, action string, proposal any) (context.Context, error) {
	l, err := a.lifecycles.GetLifecycle(ctx, organizationID, lifecycleID)
	if err != nil {
		return nil, err
	}
	return a.manage(ctx, actor, organizationID, l.WorkspaceID(), action, proposal)
}

func (a *authorized) CreateLifecycle(ctx context.Context, actor identity.Actor, in delivery.CreateLifecycleInput) (*delivery.Lifecycle, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.WorkspaceID, delivery.ActionLifecycleCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateLifecycle(ctx, actor, in)
}

func (a *authorized) GetLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	l, err := a.next.GetLifecycle(ctx, actor, organizationID, lifecycleID)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, organizationID, l.WorkspaceID(), delivery.ActionLifecycleRead); err != nil {
		return nil, err
	}
	return l, nil
}

func (a *authorized) ListLifecycles(ctx context.Context, actor identity.Actor, query delivery.ListLifecyclesQuery) (*pagination.Result[*delivery.Lifecycle], error) {
	if err := a.read(ctx, actor, query.OrganizationID, query.WorkspaceID, delivery.ActionLifecycleList); err != nil {
		return nil, err
	}
	return a.next.ListLifecycles(ctx, actor, query)
}

func (a *authorized) UpdateLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props delivery.UpdateLifecycleProps) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateLifecycle(ctx, actor, organizationID, lifecycleID, props)
}

func (a *authorized) AddLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props delivery.LifecyclePhaseProps) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleAddPhase, props)
	if err != nil {
		return nil, err
	}
	return a.next.AddLifecyclePhase(ctx, actor, organizationID, lifecycleID, props)
}

func (a *authorized) UpdateLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, props delivery.LifecyclePhaseProps) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleUpdatePhase, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateLifecyclePhase(ctx, actor, organizationID, lifecycleID, phaseID, props)
}

func (a *authorized) MoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, position int) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleMovePhase, position)
	if err != nil {
		return nil, err
	}
	return a.next.MoveLifecyclePhase(ctx, actor, organizationID, lifecycleID, phaseID, position)
}

func (a *authorized) RemoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleRemovePhase, phaseID)
	if err != nil {
		return nil, err
	}
	return a.next.RemoveLifecyclePhase(ctx, actor, organizationID, lifecycleID, phaseID)
}

func (a *authorized) ArchiveLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveLifecycle(ctx, actor, organizationID, lifecycleID)
}

func (a *authorized) RestoreLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*delivery.Lifecycle, error) {
	ctx, err := a.manageLifecycle(ctx, actor, organizationID, lifecycleID, delivery.ActionLifecycleRestore, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreLifecycle(ctx, actor, organizationID, lifecycleID)
}

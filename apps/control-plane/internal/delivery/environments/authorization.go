package environments

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Each operation is checked on the IAM resource its grants apply to: an
// Environment on itself, a new Environment or a list on its Workspace, and a
// Target on its Environment, since Targets are not IAM resources. Finding
// that resource sometimes means reading the object first; next reads it
// again in its transaction, so a decision is never applied to stale state.
type authorized struct {
	next         delivery.EnvironmentService
	authz        authorization.Authorizer
	environments delivery.EnvironmentRepository
	targets      delivery.TargetRepository
}

var _ delivery.EnvironmentService = (*authorized)(nil)

// allow asks for a decision and returns ctx carrying it for subject.
func (a *authorized) allow(ctx context.Context, subject string, req authorization.Request) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, req)
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, subject, d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string) error {
	_, err := a.allow(ctx, "", authorization.Request{
		Actor: actor, Permission: delivery.PermissionEnvironmentRead, OrganizationID: organizationID,
		ResourceID: resourceID, Action: action,
	})
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, resourceID, action string, proposal any) (context.Context, error) {
	return a.allow(ctx, "", authorization.Request{
		Actor: actor, Permission: delivery.PermissionEnvironmentManage, OrganizationID: organizationID,
		ResourceID: resourceID, Action: action, Proposal: proposal,
	})
}

func (a *authorized) CreateEnvironment(ctx context.Context, actor identity.Actor, in delivery.CreateEnvironmentInput) (*delivery.Environment, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.WorkspaceID, delivery.ActionEnvironmentCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateEnvironment(ctx, actor, in)
}

func (a *authorized) GetEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string) (*delivery.Environment, error) {
	if err := a.read(ctx, actor, organizationID, environmentID, delivery.ActionEnvironmentRead); err != nil {
		return nil, err
	}
	return a.next.GetEnvironment(ctx, actor, organizationID, environmentID)
}

// GetEnvironmentBySlug finds the Environment before authorizing, since the
// slug does not name a resource. Someone without access learns only that a
// slug is taken, which creating an Environment with it would tell them too.
func (a *authorized) GetEnvironmentBySlug(ctx context.Context, actor identity.Actor, organizationID, workspaceID, slug string) (*delivery.Environment, error) {
	e, err := a.next.GetEnvironmentBySlug(ctx, actor, organizationID, workspaceID, slug)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, organizationID, e.ID(), delivery.ActionEnvironmentRead); err != nil {
		return nil, err
	}
	return e, nil
}

func (a *authorized) ListEnvironments(ctx context.Context, actor identity.Actor, query delivery.ListEnvironmentsQuery) (*pagination.Result[*delivery.Environment], error) {
	if err := a.read(ctx, actor, query.OrganizationID, query.WorkspaceID, delivery.ActionEnvironmentList); err != nil {
		return nil, err
	}
	return a.next.ListEnvironments(ctx, actor, query)
}

func (a *authorized) UpdateEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string, props delivery.UpdateEnvironmentProps) (*delivery.Environment, error) {
	ctx, err := a.manage(ctx, actor, organizationID, environmentID, delivery.ActionEnvironmentUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateEnvironment(ctx, actor, organizationID, environmentID, props)
}

// ArchiveEnvironments authorizes each Environment on itself, and goes ahead
// only if every one is allowed.
func (a *authorized) ArchiveEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*delivery.Environment, error) {
	for _, id := range uniqueIDs(environmentIDs) {
		var err error
		ctx, err = a.allow(ctx, id, authorization.Request{
			Actor: actor, Permission: delivery.PermissionEnvironmentManage, OrganizationID: organizationID,
			ResourceID: id, Action: delivery.ActionEnvironmentArchive,
		})
		if err != nil {
			return nil, err
		}
	}
	return a.next.ArchiveEnvironments(ctx, actor, organizationID, environmentIDs)
}

// RestoreEnvironments authorizes each Environment on its Workspace, because
// grants on an archived Environment do not apply, and goes ahead only if
// every one is allowed.
func (a *authorized) RestoreEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*delivery.Environment, error) {
	ids := uniqueIDs(environmentIDs)
	found, err := a.environments.GetEnvironments(ctx, organizationID, ids)
	if err != nil {
		return nil, err
	}
	workspaceOf := make(map[string]string, len(found))
	for _, e := range found {
		workspaceOf[e.ID()] = e.WorkspaceID()
	}
	for _, id := range ids {
		ws, ok := workspaceOf[id]
		if !ok {
			return nil, fmt.Errorf("%w: environment %s", delivery.ErrNotFound, id)
		}
		ctx, err = a.allow(ctx, id, authorization.Request{
			Actor: actor, Permission: delivery.PermissionEnvironmentManage, OrganizationID: organizationID,
			ResourceID: ws, Action: delivery.ActionEnvironmentRestore, Proposal: id,
		})
		if err != nil {
			return nil, err
		}
	}
	return a.next.RestoreEnvironments(ctx, actor, organizationID, environmentIDs)
}

func (a *authorized) CreateTarget(ctx context.Context, actor identity.Actor, in delivery.CreateTargetInput) (*delivery.Target, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, in.EnvironmentID, delivery.ActionTargetCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateTarget(ctx, actor, in)
}

func (a *authorized) GetTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	t, err := a.next.GetTarget(ctx, actor, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, organizationID, t.EnvironmentID(), delivery.ActionTargetRead); err != nil {
		return nil, err
	}
	return t, nil
}

func (a *authorized) ListTargets(ctx context.Context, actor identity.Actor, query delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error) {
	if err := a.read(ctx, actor, query.OrganizationID, query.EnvironmentID, delivery.ActionTargetList); err != nil {
		return nil, err
	}
	return a.next.ListTargets(ctx, actor, query)
}

// manageTarget authorizes a change to a Target on its Environment. A Target
// never moves between Environments, so the Environment read here is the one
// the change applies in.
func (a *authorized) manageTarget(ctx context.Context, actor identity.Actor, organizationID, targetID, action string, proposal any) (context.Context, error) {
	t, err := a.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	return a.manage(ctx, actor, organizationID, t.EnvironmentID(), action, proposal)
}

func (a *authorized) UpdateTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string, props delivery.UpdateTargetProps) (*delivery.Target, error) {
	ctx, err := a.manageTarget(ctx, actor, organizationID, targetID, delivery.ActionTargetUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateTarget(ctx, actor, organizationID, targetID, props)
}

func (a *authorized) ArchiveTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	ctx, err := a.manageTarget(ctx, actor, organizationID, targetID, delivery.ActionTargetArchive, nil)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveTarget(ctx, actor, organizationID, targetID)
}

func (a *authorized) RestoreTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*delivery.Target, error) {
	ctx, err := a.manageTarget(ctx, actor, organizationID, targetID, delivery.ActionTargetRestore, nil)
	if err != nil {
		return nil, err
	}
	return a.next.RestoreTarget(ctx, actor, organizationID, targetID)
}

package locks

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// A lock is authorized at its level, on the level's IAM resource: the
// Organization, or the Workspace. Adding or removing one needs
// policy.manage, which admins hold, and listing needs policy.read. A key
// that cannot be locked at the level is refused before asking, since that
// reads only the setting registry; whether the Workspace is active is
// checked after, in next's transaction.
type authorized struct {
	next        policy.SettingLockService
	authz       authorization.Authorizer
	definitions policy.SettingDefinitions
}

var _ policy.SettingLockService = (*authorized)(nil)

// manage asks about adding or removing a lock and returns ctx carrying the
// decision for the call.
func (a *authorized) manage(ctx context.Context, actor identity.Actor, in policy.SettingLockInput, action string) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: policy.PermissionPolicyManage, OrganizationID: in.Scope.OrganizationID,
		ResourceID: in.Scope.ResourceID(), Action: action, Proposal: in,
	})
	if err != nil {
		return nil, err
	}
	// A lock changes which setting values apply, so an agent Session needs a
	// human's approval to add or remove one, as it does for a setting (ADR
	// 0007). Refuse if a decision ever says otherwise.
	if actor.IsAgent() && d.ApprovedByPrincipalID == "" {
		return nil, &authorization.DeniedError{DecisionID: d.ID, Reason: "an agent session's setting lock change needs a human's approval"}
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) ListSettingLocks(ctx context.Context, actor identity.Actor, scope policy.LockScope) ([]*policy.SettingLock, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if _, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: policy.PermissionPolicyRead, OrganizationID: scope.OrganizationID,
		ResourceID: scope.ResourceID(), Action: policy.ActionSettingLockList,
	}); err != nil {
		return nil, err
	}
	return a.next.ListSettingLocks(ctx, actor, scope)
}

func (a *authorized) AddSettingLock(ctx context.Context, actor identity.Actor, in policy.SettingLockInput) (*policy.SettingLock, error) {
	if err := policy.ValidateLockKey(a.definitions, in.Scope, in.Key); err != nil {
		return nil, err
	}
	ctx, err := a.manage(ctx, actor, in, policy.ActionSettingLockAdd)
	if err != nil {
		return nil, err
	}
	return a.next.AddSettingLock(ctx, actor, in)
}

// RemoveSettingLock does not check the key against the registry, so a lock
// whose setting is no longer defined can still be removed.
func (a *authorized) RemoveSettingLock(ctx context.Context, actor identity.Actor, in policy.SettingLockInput) error {
	if err := in.Scope.Validate(); err != nil {
		return err
	}
	ctx, err := a.manage(ctx, actor, in, policy.ActionSettingLockRemove)
	if err != nil {
		return err
	}
	return a.next.RemoveSettingLock(ctx, actor, in)
}

package settings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Settings are checked on their level's resource: the Organization,
// Workspace, or Project. A change or removal asks for the permission the
// key's definition names, with the proposed change, including the value it
// replaces, as the proposal; reading that value means reading the stored
// Setting first. A change made against a version that is no longer current
// is refused then, so no approval is ever requested for a change that
// cannot apply; next reads the Setting again, in its transaction, and checks
// once more. Input that can never be valid (an unknown key, a level the
// definition does not allow, a value that is not JSON or too large) is
// refused before asking. The owning capability's validation of the value,
// and the checks that the Workspace and Project are active, may read other
// records, so next runs them after the decision.
type authorized struct {
	next        workspace.SettingService
	authz       authorization.Authorizer
	repo        workspace.Repository
	definitions *workspace.Definitions
}

var _ workspace.SettingService = (*authorized)(nil)

// allowChange asks about a change or removal and returns ctx carrying the
// decision for the call.
func (a *authorized) allowChange(ctx context.Context, actor identity.Actor, def workspace.Definition, change workspace.SettingChange, action, approvalID string) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: def.Permission, OrganizationID: change.Scope.OrganizationID,
		ResourceID: change.Scope.ResourceID(), Action: action, Proposal: change, ApprovalID: approvalID,
	})
	if err != nil {
		return nil, err
	}
	// Policy enforces that agent Sessions never change settings without a
	// human's approval (ADR 0007). Refuse here too if a decision ever says otherwise.
	if actor.IsAgent() && d.ApprovedByPrincipalID == "" {
		return nil, &authorization.DeniedError{DecisionID: d.ID, Reason: "an agent session's setting change needs a human's approval"}
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, scope workspace.Scope, action string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: workspace.PermissionSettingRead, OrganizationID: scope.OrganizationID,
		ResourceID: scope.ResourceID(), Action: action,
	})
	return err
}

func (a *authorized) ChangeSetting(ctx context.Context, actor identity.Actor, in workspace.ChangeSettingInput) (*workspace.Setting, error) {
	def, err := a.definitions.ForScope(in.Scope, in.Key)
	if err != nil {
		return nil, err
	}
	if err := workspace.ValidateSettingValue(in.Value); err != nil {
		return nil, err
	}
	current, err := currentSetting(ctx, a.repo, in.Scope, in.Key, in.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	var before json.RawMessage
	if current != nil {
		before = current.Value()
	}
	ctx, err = a.allowChange(ctx, actor, def, workspace.SettingChange{
		Scope: in.Scope, Key: in.Key, Level: in.Scope.Level(),
		Before: before, Value: in.Value, ExpectedVersion: in.ExpectedVersion,
	}, workspace.ActionSettingChange, in.ApprovalID)
	if err != nil {
		return nil, err
	}
	return a.next.ChangeSetting(ctx, actor, in)
}

func (a *authorized) RemoveSetting(ctx context.Context, actor identity.Actor, in workspace.RemoveSettingInput) error {
	def, err := a.definitions.ForScope(in.Scope, in.Key)
	if err != nil {
		return err
	}
	current, err := currentSetting(ctx, a.repo, in.Scope, in.Key, in.ExpectedVersion)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("setting %s at %s level: %w", in.Key, in.Scope.Level(), workspace.ErrNotFound)
	}
	ctx, err = a.allowChange(ctx, actor, def, workspace.SettingChange{
		Scope: in.Scope, Key: in.Key, Level: in.Scope.Level(),
		Before: current.Value(), ExpectedVersion: in.ExpectedVersion,
	}, workspace.ActionSettingRemove, in.ApprovalID)
	if err != nil {
		return err
	}
	return a.next.RemoveSetting(ctx, actor, in)
}

func (a *authorized) ListSettings(ctx context.Context, actor identity.Actor, scope workspace.Scope) ([]*workspace.Setting, error) {
	if err := a.read(ctx, actor, scope, workspace.ActionSettingList); err != nil {
		return nil, err
	}
	return a.next.ListSettings(ctx, actor, scope)
}

func (a *authorized) ResolveSetting(ctx context.Context, actor identity.Actor, scope workspace.Scope, key string) (workspace.Resolved, error) {
	if err := a.read(ctx, actor, scope, workspace.ActionSettingRead); err != nil {
		return workspace.Resolved{}, err
	}
	return a.next.ResolveSetting(ctx, actor, scope, key)
}

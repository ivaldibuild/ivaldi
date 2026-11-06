package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// SettingLifecycles reads the Lifecycle a setting names.
// LifecycleRepository provides it.
type SettingLifecycles interface {
	GetLifecycle(ctx context.Context, organizationID, lifecycleID string) (*Lifecycle, error)
}

// RegisterSettings registers the settings delivery defines with the
// workspace capability's registry, at startup:
//
//   - delivery.default_lifecycle: the Lifecycle a new Application's default
//     Channel follows when its creator names none. Lifecycles belong to a
//     Workspace, so it is set for a Workspace or a Project, never the
//     Organization, and its value must be an active Lifecycle in that
//     Workspace. It has no default. Changing it needs lifecycle.manage,
//     the permission for deciding the paths Releases take through a
//     Workspace's Environments, on the Workspace or Project.
func RegisterSettings(defs *workspace.Definitions, lifecycles SettingLifecycles) error {
	if defs == nil {
		return errors.New("delivery settings: Definitions are required")
	}
	if lifecycles == nil {
		return errors.New("delivery settings: SettingLifecycles is required")
	}
	if err := defs.Register(workspace.Definition{
		Key:         SettingDefaultLifecycle,
		Description: "The Lifecycle a new Application's default Channel follows when none is named",
		Levels:      []workspace.Level{workspace.LevelWorkspace, workspace.LevelProject},
		Permission:  PermissionLifecycleManage,
		Validate:    validateDefaultLifecycle(lifecycles),
	}); err != nil {
		return fmt.Errorf("delivery settings: %w", err)
	}
	return nil
}

// validateDefaultLifecycle checks that a delivery.default_lifecycle value
// names an active Lifecycle in the scope's Workspace. A refusal matches
// workspace.ErrInvalid, so the workspace capability shows it to the caller;
// a failed lookup does not, so it is treated as unexpected.
func validateDefaultLifecycle(lifecycles SettingLifecycles) func(context.Context, workspace.Scope, json.RawMessage) error {
	return func(ctx context.Context, scope workspace.Scope, value json.RawMessage) error {
		lifecycleID, err := lifecycleIDFrom(value)
		if err != nil {
			return err
		}
		l, err := lifecycles.GetLifecycle(ctx, scope.OrganizationID, lifecycleID)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: lifecycle %s does not exist", workspace.ErrInvalid, lifecycleID)
		}
		if err != nil {
			return fmt.Errorf("look up lifecycle %s: %w", lifecycleID, err)
		}
		if l.WorkspaceID() != scope.WorkspaceID {
			return fmt.Errorf("%w: lifecycle %s is not in workspace %s", workspace.ErrInvalid, lifecycleID, scope.WorkspaceID)
		}
		if l.Archived() {
			return fmt.Errorf("%w: lifecycle %s is archived", workspace.ErrInvalid, lifecycleID)
		}
		return nil
	}
}

// DefaultLifecycleID returns the Lifecycle ID a resolved
// delivery.default_lifecycle names, or "" when it has no value.
func DefaultLifecycleID(r workspace.Resolved) (string, error) {
	if len(r.Value) == 0 || string(r.Value) == "null" {
		return "", nil
	}
	lifecycleID, err := lifecycleIDFrom(r.Value)
	if err != nil {
		return "", fmt.Errorf("setting %s: %w", SettingDefaultLifecycle, err)
	}
	return lifecycleID, nil
}

func lifecycleIDFrom(value json.RawMessage) (string, error) {
	var lifecycleID string
	if err := json.Unmarshal(value, &lifecycleID); err != nil {
		return "", fmt.Errorf("%w: must be a lifecycle ID, as a JSON string", workspace.ErrInvalid)
	}
	if !id.Valid(Lifecycle{}.Prefix(), lifecycleID) {
		return "", fmt.Errorf("%w: %q is not a lifecycle ID", workspace.ErrInvalid, lifecycleID)
	}
	return lifecycleID, nil
}

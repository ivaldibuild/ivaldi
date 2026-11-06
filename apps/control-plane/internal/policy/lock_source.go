package policy

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// LocksInEffect reads the locks that apply at a scope.
// SettingLockRepository provides it.
type LocksInEffect interface {
	ListLocksInEffect(ctx context.Context, scope LockScope) ([]*SettingLock, error)
}

// NewLockSource returns the workspace.LockSource the workspace capability
// reads locks through. It reports the Organization's locks and, when a
// Workspace is named, that Workspace's; when both lock a key, it reports
// LevelOrganization, because an Organization lock wins.
func NewLockSource(locks LocksInEffect) workspace.LockSource {
	return lockSource{locks: locks}
}

type lockSource struct {
	locks LocksInEffect
}

func (s lockSource) LockedKeys(ctx context.Context, organizationID, workspaceID string) (map[string]workspace.Level, error) {
	found, err := s.locks.ListLocksInEffect(ctx, LockScope{OrganizationID: organizationID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list setting locks: %w", err)
	}
	out := make(map[string]workspace.Level, len(found))
	for _, l := range found {
		if l.Level() == workspace.LevelOrganization || out[l.Key()] == "" {
			out[l.Key()] = l.Level()
		}
	}
	return out, nil
}

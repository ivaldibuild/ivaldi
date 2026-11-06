package policy

// The interfaces this capability defines for itself: its repository, what
// it needs from other capabilities, and its service.

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SettingLockRepository persists and retrieves setting locks, and nothing
// more. Every method is scoped to an Organization. Methods return
// ErrNotFound when no row matches, and ErrConflict when a lock on the key
// already exists at the level.
type SettingLockRepository interface {
	CreateSettingLock(ctx context.Context, l *SettingLock) error
	// GetSettingLock returns the lock on key at exactly the scope's level.
	GetSettingLock(ctx context.Context, scope LockScope, key string) (*SettingLock, error)
	// ListSettingLocks returns the locks set at exactly the scope's level,
	// ordered by key. There is at most one per setting definition, so the
	// list is not paged.
	ListSettingLocks(ctx context.Context, scope LockScope) ([]*SettingLock, error)
	// ListLocksInEffect returns the locks that apply at the scope: the
	// Organization's and, for a Workspace scope, that Workspace's.
	ListLocksInEffect(ctx context.Context, scope LockScope) ([]*SettingLock, error)
	// DeleteSettingLock removes a lock. History is kept by its audit record
	// and its removed event, not by the table.
	DeleteSettingLock(ctx context.Context, organizationID, lockID string) error
}

// Workspaces looks up Workspaces in the workspace capability, without
// authorizing, so a Workspace lock is only added to an active Workspace.
// workspace.NewLookups implements it.
type Workspaces interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error)
}

// SettingLockService adds, removes, and lists setting locks on behalf of an
// actor. The locks sub-package implements it, and serves it over HTTP. Its
// constructor returns the service wrapped in an authorizing layer, so the
// interface is the only way in. The workspace capability reads locks
// through NewLockSource, which does not authorize.
type SettingLockService interface {
	// ListSettingLocks returns the locks set at exactly one level.
	ListSettingLocks(ctx context.Context, actor identity.Actor, scope LockScope) ([]*SettingLock, error)
	// AddSettingLock locks a key at an Organization or an active Workspace.
	// Adding a lock that exists returns it and changes nothing.
	AddSettingLock(ctx context.Context, actor identity.Actor, in SettingLockInput) (*SettingLock, error)
	// RemoveSettingLock removes a lock, so the values below its level apply again.
	RemoveSettingLock(ctx context.Context, actor identity.Actor, in SettingLockInput) error
}

// SettingLockInput names a lock: a setting key at a level.
type SettingLockInput struct {
	Scope LockScope
	Key   string
}

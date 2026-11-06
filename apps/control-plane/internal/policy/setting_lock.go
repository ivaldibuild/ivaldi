package policy

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// LockScope is where a setting lock applies: the whole Organization, or one
// Workspace in it and its Projects. Projects cannot lock settings.
type LockScope struct {
	OrganizationID string `json:"organization_id"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
}

// OrganizationLockScope and WorkspaceLockScope build scopes.
func OrganizationLockScope(organizationID string) LockScope {
	return LockScope{OrganizationID: organizationID}
}

func WorkspaceLockScope(organizationID, workspaceID string) LockScope {
	return LockScope{OrganizationID: organizationID, WorkspaceID: workspaceID}
}

// Level is the level the lock is set at, and the level values resolve at
// while it holds.
func (s LockScope) Level() workspace.Level {
	if s.WorkspaceID != "" {
		return workspace.LevelWorkspace
	}
	return workspace.LevelOrganization
}

// ResourceID is the IAM resource a lock at this scope is authorized on: the
// Organization, or the Workspace.
func (s LockScope) ResourceID() string {
	if s.WorkspaceID != "" {
		return s.WorkspaceID
	}
	return s.OrganizationID
}

// Validate checks that the scope names an Organization.
func (s LockScope) Validate() error {
	if s.OrganizationID == "" {
		return NewErrInvalidInput("a setting lock needs an organization")
	}
	return nil
}

// SettingLock fixes a setting key at an Organization or a Workspace: values
// below its level are ignored when the key is resolved, and changes to them
// are refused. Locks do not change; one is added, and later removed.
type SettingLock struct {
	aggregate.Root
	scope               LockScope
	key                 string
	lockedByPrincipalID string
	createdAt           time.Time
}

// SettingLockSnapshot is a SettingLock's state as plain values, for
// repositories, events, and audit records. Changing it does not change the lock.
type SettingLockSnapshot struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	// WorkspaceID is empty for an Organization lock.
	WorkspaceID string          `json:"workspace_id"`
	Level       workspace.Level `json:"level"`
	Key         string          `json:"key"`
	// LockedByPrincipalID is who added the lock.
	LockedByPrincipalID string    `json:"locked_by_principal_id"`
	CreatedAt           time.Time `json:"created_at"`
}

// NewSettingLockProps describes a SettingLock. A new one leaves CreatedAt
// nil; a repository loading a stored one sets it.
type NewSettingLockProps struct {
	Scope               LockScope
	Key                 string
	LockedByPrincipalID string
	CreatedAt           *time.Time
}

// NewSettingLock returns a SettingLock, with a new ID unless lockID gives a
// stored one, after checking its scope, the key's format, and who locked it.
// Whether the key is a setting that can be locked at the scope is checked by
// ValidateLockKey, since it needs the registered definitions; a lock stays
// loadable after its definition is removed, so it can still be removed.
func NewSettingLock(p NewSettingLockProps, lockID *string) (*SettingLock, error) {
	if err := p.Scope.Validate(); err != nil {
		return nil, err
	}
	root, err := newRoot(SettingLock{}.Prefix(), lockID)
	if err != nil {
		return nil, err
	}
	if err := workspace.ValidateSettingKey(p.Key); err != nil {
		return nil, errorf("setting key %q must be namespaced, such as capability.name", p.Key)
	}
	if p.LockedByPrincipalID == "" {
		return nil, NewErrInvalidInput("a setting lock needs the principal that added it")
	}

	l := &SettingLock{
		Root:                root,
		scope:               p.Scope,
		key:                 p.Key,
		lockedByPrincipalID: p.LockedByPrincipalID,
		createdAt:           timeOr(p.CreatedAt, clock.Now()),
	}
	if l.IsNew() {
		l.RecordEvent(SettingLockAdded{l.Snapshot()})
	}
	return l, nil
}

// Prefix is the type prefix of SettingLock IDs.
func (SettingLock) Prefix() string { return "slk" }

func (l *SettingLock) OrganizationID() string      { return l.scope.OrganizationID }
func (l *SettingLock) WorkspaceID() string         { return l.scope.WorkspaceID }
func (l *SettingLock) Scope() LockScope            { return l.scope }
func (l *SettingLock) Level() workspace.Level      { return l.scope.Level() }
func (l *SettingLock) Key() string                 { return l.key }
func (l *SettingLock) LockedByPrincipalID() string { return l.lockedByPrincipalID }
func (l *SettingLock) CreatedAt() time.Time        { return l.createdAt }

// Snapshot returns the lock's state.
func (l *SettingLock) Snapshot() SettingLockSnapshot {
	return SettingLockSnapshot{
		ID:                  l.ID(),
		OrganizationID:      l.scope.OrganizationID,
		WorkspaceID:         l.scope.WorkspaceID,
		Level:               l.scope.Level(),
		Key:                 l.key,
		LockedByPrincipalID: l.lockedByPrincipalID,
		CreatedAt:           l.createdAt,
	}
}

// Remove records that the lock is being removed, so values below its level
// apply again. The caller deletes it.
func (l *SettingLock) Remove(removedBy string) {
	l.RecordEvent(SettingLockRemoved{SettingLock: l.Snapshot(), RemovedByPrincipalID: removedBy})
}

// SettingDefinitions looks up the setting definitions capabilities register.
// *workspace.Definitions implements it.
type SettingDefinitions interface {
	Lookup(key string) (workspace.Definition, bool)
}

// ValidateLockKey checks that key is a registered setting that may be set
// below the scope's level. A lock on a setting that can only be set at the
// lock's level, or above it, would fix nothing.
func ValidateLockKey(definitions SettingDefinitions, scope LockScope, key string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	def, ok := definitions.Lookup(key)
	if !ok {
		return errorf("no capability defines the setting %q", key)
	}
	for _, l := range def.Levels {
		if l.Below(scope.Level()) {
			return nil
		}
	}
	return errorf("setting %s cannot be set below %s level, so a lock there would fix nothing", key, scope.Level())
}

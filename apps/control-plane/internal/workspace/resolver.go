package workspace

import (
	"context"
	"fmt"
)

// SettingResolver reads settings' effective values for other capabilities,
// such as delivery finding the Lifecycle a new Channel starts with. It does
// not authorize: a capability reading its own configuration needs no
// actor's permission.
type SettingResolver interface {
	// ResolveSetting returns a setting's effective value for a scope: the
	// most specific stored value, unless an Organization or Workspace lock
	// fixes it at a higher level, and otherwise the definition's default.
	ResolveSetting(ctx context.Context, scope Scope, key string) (Resolved, error)
}

// NewSettingResolver returns a SettingResolver that reads values through
// repo, definitions from definitions, and locks from locks, which the
// policy capability provides.
func NewSettingResolver(repo Repository, definitions *Definitions, locks LockSource) (SettingResolver, error) {
	switch {
	case repo == nil:
		return nil, fmt.Errorf("setting resolver: a Repository is required")
	case definitions == nil:
		return nil, fmt.Errorf("setting resolver: Definitions are required")
	case locks == nil:
		return nil, fmt.Errorf("setting resolver: a LockSource is required")
	}
	return &resolver{repo: repo, definitions: definitions, locks: locks}, nil
}

type resolver struct {
	repo        Repository
	definitions *Definitions
	locks       LockSource
}

func (r *resolver) ResolveSetting(ctx context.Context, scope Scope, key string) (Resolved, error) {
	if err := scope.Validate(); err != nil {
		return Resolved{}, err
	}
	def, ok := r.definitions.Lookup(key)
	if !ok {
		return Resolved{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	stored, err := r.repo.SettingsForResolution(ctx, scope, key)
	if err != nil {
		return Resolved{}, fmt.Errorf("read setting %s: %w", key, err)
	}
	locked, err := r.locks.LockedKeys(ctx, scope.OrganizationID, scope.WorkspaceID)
	if err != nil {
		return Resolved{}, fmt.Errorf("read setting locks: %w", err)
	}
	return resolve(def, stored, locked[key]), nil
}

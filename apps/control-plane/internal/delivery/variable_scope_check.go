package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// VariableScopeChecker checks what Variables' values name: that every
// Environment, Target, and Channel a scope names is active and the
// Variable's to use, and that every Secret Store a secret value is kept in is
// active, in its Workspace, and allowed for the Environments the value names.
// A save and an Application's restore both use it, so a restored Variable is
// checked exactly as a saved one was.
//
// It reads the records under their rows' locks, each table once and in ID
// order: the Targets, then their Environments, then the Secret Stores. The
// archives that count what names a record take the same row first, so
// whichever runs first, the other sees it. Callers hold the Variable's
// Project's variables and, for an Application's Variable, the Application's
// row, which is what keeps its Channels from changing meanwhile.
type VariableScopeChecker interface {
	// Check checks every scope of every Variable given, and the stores their
	// secret values are kept in. An error names the Variable it is about.
	Check(ctx context.Context, workspaceID string, variables ...*Variable) error
	// CheckScope checks the one scope a change to v names, and the stores
	// v's secret values are kept in.
	CheckScope(ctx context.Context, workspaceID string, v *Variable, scope VariableScope) error
}

// VariableTargetLookup reads the Targets a Variable's values are scoped to,
// locked so they cannot be archived while the Variable is checked.
// delivery.TargetRepository provides it.
type VariableTargetLookup interface {
	GetTargetsForUpdate(ctx context.Context, organizationID string, targetIDs []string) ([]*Target, error)
}

// VariableEnvironmentLookup reads the Environments a Variable's values are
// scoped to, locked so they cannot be archived while the Variable is checked.
// delivery.EnvironmentRepository provides it.
type VariableEnvironmentLookup interface {
	GetEnvironmentsForUpdate(ctx context.Context, organizationID string, environmentIDs []string) ([]*Environment, error)
}

// VariableChannelLookup reads the Channels a Variable's values are scoped to.
// delivery.ApplicationRepository provides it.
type VariableChannelLookup interface {
	GetChannel(ctx context.Context, organizationID, channelID string) (*Channel, error)
}

// VariableStoreLookup reads the Secret Stores a Variable's values are kept
// in, held so they cannot be archived while the Variable is checked.
// delivery.SecretStores provides it.
type VariableStoreLookup interface {
	LookupSecretStoresForShare(ctx context.Context, organizationID string, storeIDs []string) (map[string]SecretStoreRef, error)
}

// NewVariableScopeChecker returns the checks over the lookups given, or an
// error naming a missing one.
func NewVariableScopeChecker(targets VariableTargetLookup, environments VariableEnvironmentLookup, channels VariableChannelLookup, stores VariableStoreLookup) (VariableScopeChecker, error) {
	required := []struct {
		name string
		ok   bool
	}{
		{"VariableTargetLookup", targets != nil},
		{"VariableEnvironmentLookup", environments != nil},
		{"VariableChannelLookup", channels != nil},
		{"VariableStoreLookup", stores != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("variable scope checker: %s is required", r.name)
		}
	}
	return &scopeChecker{targets: targets, environments: environments, channels: channels, stores: stores}, nil
}

type scopeChecker struct {
	targets      VariableTargetLookup
	environments VariableEnvironmentLookup
	channels     VariableChannelLookup
	stores       VariableStoreLookup
}

// scopeCheck is one scope to check, and the Variable it belongs to.
type scopeCheck struct {
	v     *Variable
	scope VariableScope
}

// lockedRows are the records a set of checks reads, by ID. Records that do
// not exist are left out.
type lockedRows struct {
	targets      map[string]*Target
	environments map[string]*Environment
	stores       map[string]SecretStoreRef
}

func (c *scopeChecker) Check(ctx context.Context, workspaceID string, variables ...*Variable) error {
	checks := make([]scopeCheck, len(variables))
	for i, v := range variables {
		checks[i] = scopeCheck{v: v, scope: v.Scope()}
	}
	rows, err := c.lock(ctx, checks)
	if err != nil {
		return err
	}
	for _, ch := range checks {
		if err := c.verify(ctx, workspaceID, ch, rows); err != nil {
			return fmt.Errorf("variable %s: %w", ch.v.Name(), err)
		}
	}
	return nil
}

func (c *scopeChecker) CheckScope(ctx context.Context, workspaceID string, v *Variable, scope VariableScope) error {
	ch := scopeCheck{v: v, scope: scope}
	rows, err := c.lock(ctx, []scopeCheck{ch})
	if err != nil {
		return err
	}
	return c.verify(ctx, workspaceID, ch, rows)
}

// lock reads everything the checks name, one statement per table: every
// Target any of the Variables' values names (the stores' checks need their
// Environments), every Environment a checked scope names directly or through
// a Target, and every store a secret value is kept in.
func (c *scopeChecker) lock(ctx context.Context, checks []scopeCheck) (lockedRows, error) {
	rows := lockedRows{targets: map[string]*Target{}, environments: map[string]*Environment{}, stores: map[string]SecretStoreRef{}}
	if len(checks) == 0 {
		return rows, nil
	}
	organizationID := checks[0].v.OrganizationID()

	var targetIDs []string
	for _, ch := range checks {
		targetIDs = append(targetIDs, ch.v.Scope().TargetIDs...)
		targetIDs = append(targetIDs, ch.scope.TargetIDs...)
	}
	if targetIDs = sortedUnique(targetIDs); len(targetIDs) > 0 {
		targets, err := c.targets.GetTargetsForUpdate(ctx, organizationID, targetIDs)
		if err != nil {
			return lockedRows{}, fmt.Errorf("get targets: %w", err)
		}
		for _, t := range targets {
			rows.targets[t.ID()] = t
		}
	}

	var environmentIDs []string
	for _, ch := range checks {
		environmentIDs = append(environmentIDs, ch.scope.EnvironmentIDs...)
		for _, id := range ch.scope.TargetIDs {
			if t, ok := rows.targets[id]; ok {
				environmentIDs = append(environmentIDs, t.EnvironmentID())
			}
		}
	}
	if environmentIDs = sortedUnique(environmentIDs); len(environmentIDs) > 0 {
		envs, err := c.environments.GetEnvironmentsForUpdate(ctx, organizationID, environmentIDs)
		if err != nil {
			return lockedRows{}, fmt.Errorf("get environments: %w", err)
		}
		for _, e := range envs {
			rows.environments[e.ID()] = e
		}
	}

	var storeIDs []string
	for _, ch := range checks {
		storeIDs = append(storeIDs, ch.v.SecretStoreIDs()...)
	}
	if storeIDs = sortedUnique(storeIDs); len(storeIDs) > 0 {
		stores, err := c.stores.LookupSecretStoresForShare(ctx, organizationID, storeIDs)
		if err != nil {
			return lockedRows{}, fmt.Errorf("look up secret stores: %w", err)
		}
		rows.stores = stores
	}
	return rows, nil
}

// verify checks one scope against the rows read for it, then the stores its
// Variable's secret values are kept in.
func (c *scopeChecker) verify(ctx context.Context, workspaceID string, ch scopeCheck, rows lockedRows) error {
	if err := c.checkScope(ctx, workspaceID, ch.v, ch.scope, rows); err != nil {
		return err
	}
	return checkStores(workspaceID, ch.v, rows)
}

// checkScope checks that everything scope names is active and the Variable's
// to use: Environments and Targets in its Workspace, and Channels of its
// Application.
func (c *scopeChecker) checkScope(ctx context.Context, workspaceID string, v *Variable, scope VariableScope, rows lockedRows) error {
	environmentIDs := slices.Clone(scope.EnvironmentIDs)
	for _, targetID := range scope.TargetIDs {
		t, ok := rows.targets[targetID]
		switch {
		case !ok:
			return NewErrInvalidInput(fmt.Sprintf("target %s does not exist", targetID))
		case t.Archived():
			return fmt.Errorf("%w: target %s", ErrArchived, targetID)
		case !slices.Contains(environmentIDs, t.EnvironmentID()):
			environmentIDs = append(environmentIDs, t.EnvironmentID())
		}
	}
	for _, id := range environmentIDs {
		e, ok := rows.environments[id]
		switch {
		case !ok:
			return NewErrInvalidInput(fmt.Sprintf("environment %s does not exist", id))
		case e.WorkspaceID() != workspaceID:
			return fmt.Errorf("%w: environment %s is not in workspace %s", ErrWorkspaceMismatch, id, workspaceID)
		case e.Archived():
			return fmt.Errorf("%w: environment %s", ErrArchived, id)
		}
	}
	for _, channelID := range scope.ChannelIDs {
		ch, err := c.channels.GetChannel(ctx, v.OrganizationID(), channelID)
		if errors.Is(err, ErrNotFound) || (err == nil && !sameID(v.ApplicationID(), ch.ApplicationID())) {
			return NewErrInvalidInput(fmt.Sprintf("channel %s is not one of the application's channels", channelID))
		}
		if err != nil {
			return fmt.Errorf("get channel %s: %w", channelID, err)
		}
		if ch.Archived() {
			return fmt.Errorf("%w: channel %s", ErrArchived, channelID)
		}
	}
	return nil
}

// checkStores checks the stores a secret Variable's values are kept in: each
// is active and in the Variable's Workspace, and may be read from every
// Environment its value names, directly or through a Target. A value that
// names no Environment is checked when it is deployed, against the
// Environment it is deployed to.
func checkStores(workspaceID string, v *Variable, rows lockedRows) error {
	for _, value := range v.Values() {
		if value.Secret == nil {
			continue
		}
		store, ok := rows.stores[value.Secret.StoreID]
		switch {
		case !ok:
			return NewErrInvalidInput(fmt.Sprintf("secret store %s does not exist", value.Secret.StoreID))
		case store.WorkspaceID != workspaceID:
			return fmt.Errorf("%w: secret store %s is not in workspace %s", ErrWorkspaceMismatch, store.ID, workspaceID)
		case store.Archived:
			return fmt.Errorf("%w: secret store %s", ErrArchived, store.ID)
		}
		environmentIDs := slices.Clone(value.Scope.EnvironmentIDs)
		for _, targetID := range value.Scope.TargetIDs {
			t, ok := rows.targets[targetID]
			if !ok {
				return NewErrInvalidInput(fmt.Sprintf("target %s does not exist", targetID))
			}
			environmentIDs = append(environmentIDs, t.EnvironmentID())
		}
		for _, environmentID := range environmentIDs {
			if !store.Allows(environmentID) {
				return NewErrInvalidInput(fmt.Sprintf(
					"secret store %s is not allowed for environment %s, which the value is scoped to", store.DisplayName, environmentID))
			}
		}
	}
	return nil
}

// sortedUnique returns the IDs sorted and without repeats, so rows are taken
// in one order however the caller listed them.
func sortedUnique(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

func sameID(a *string, b string) bool { return a != nil && *a == b }

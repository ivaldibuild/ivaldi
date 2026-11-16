package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// VariableTargetLookup reads the Targets a Variable's values are scoped to,
// locked so they cannot be archived while the Variable is checked.
// delivery.TargetRepository provides it.
type VariableTargetLookup interface {
	GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*Target, error)
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

// VariableScopeChecks checks what a Variable's values name: that every
// Environment, Target, and Channel a scope names is active and the Variable's
// to use, and that every Secret Store a secret value is kept in is active,
// in its Workspace, and allowed for the Environments the value names. A save
// and restoring an Application both use it, so a restored Variable is checked
// exactly as a saved one was.
type VariableScopeChecks struct {
	targets      VariableTargetLookup
	environments VariableEnvironmentLookup
	channels     VariableChannelLookup
	stores       SecretStores
}

// NewVariableScopeChecks returns the checks over the lookups given, or an
// error naming a missing one.
func NewVariableScopeChecks(targets VariableTargetLookup, environments VariableEnvironmentLookup, channels VariableChannelLookup, stores SecretStores) (*VariableScopeChecks, error) {
	required := []struct {
		name string
		ok   bool
	}{
		{"VariableTargetLookup", targets != nil},
		{"VariableEnvironmentLookup", environments != nil},
		{"VariableChannelLookup", channels != nil},
		{"SecretStores", stores != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("variable scope checks: %s is required", r.name)
		}
	}
	return &VariableScopeChecks{targets: targets, environments: environments, channels: channels, stores: stores}, nil
}

// Check checks every scope of every Variable given, and the stores their
// secret values are kept in. All the Targets are read and locked first, so a
// restore of many Variables takes its rows in one order.
func (c *VariableScopeChecks) Check(ctx context.Context, workspaceID string, variables ...*Variable) error {
	var targetIDs []string
	for _, v := range variables {
		targetIDs = append(targetIDs, v.Scope().TargetIDs...)
	}
	organizationID := ""
	if len(variables) > 0 {
		organizationID = variables[0].OrganizationID()
	}
	targets, err := c.lockTargets(ctx, organizationID, targetIDs)
	if err != nil {
		return err
	}
	for _, v := range variables {
		if err := c.checkScope(ctx, v, workspaceID, v.Scope(), targets); err != nil {
			return err
		}
		if err := c.checkStores(ctx, v, workspaceID, targets); err != nil {
			return err
		}
	}
	return nil
}

// CheckScope checks the one scope a change names, and the stores a Variable's
// secret values are kept in. Every Target the Variable names is read and
// locked, not only the ones scope names, so a save always takes its rows in
// one order.
func (c *VariableScopeChecks) CheckScope(ctx context.Context, workspaceID string, v *Variable, scope VariableScope) error {
	targets, err := c.lockTargets(ctx, v.OrganizationID(), append(slices.Clone(v.Scope().TargetIDs), scope.TargetIDs...))
	if err != nil {
		return err
	}
	if err := c.checkScope(ctx, v, workspaceID, scope, targets); err != nil {
		return err
	}
	return c.checkStores(ctx, v, workspaceID, targets)
}

// checkScope checks that everything scope names is active and the Variable's
// to use: Environments and Targets in its Workspace, and Channels of its
// Application. Callers hold the Variable's Application, if it has one, so a
// value scoped to a Channel cannot outlive the Channel.
func (c *VariableScopeChecks) checkScope(ctx context.Context, v *Variable, workspaceID string, scope VariableScope, targets map[string]*Target) error {
	organizationID := v.OrganizationID()
	environmentIDs := slices.Clone(scope.EnvironmentIDs)
	for _, targetID := range scope.TargetIDs {
		t, ok := targets[targetID]
		switch {
		case !ok:
			return NewErrInvalidInput(fmt.Sprintf("target %s does not exist", targetID))
		case t.Archived():
			return fmt.Errorf("%w: target %s", ErrArchived, targetID)
		case !slices.Contains(environmentIDs, t.EnvironmentID()):
			environmentIDs = append(environmentIDs, t.EnvironmentID())
		}
	}
	if len(environmentIDs) > 0 {
		envs, err := c.environments.GetEnvironmentsForUpdate(ctx, organizationID, environmentIDs)
		if err != nil {
			return fmt.Errorf("get environments: %w", err)
		}
		byID := make(map[string]*Environment, len(envs))
		for _, e := range envs {
			byID[e.ID()] = e
		}
		for _, id := range environmentIDs {
			e, ok := byID[id]
			switch {
			case !ok:
				return NewErrInvalidInput(fmt.Sprintf("environment %s does not exist", id))
			case e.WorkspaceID() != workspaceID:
				return fmt.Errorf("%w: environment %s is not in workspace %s", ErrWorkspaceMismatch, id, workspaceID)
			case e.Archived():
				return fmt.Errorf("%w: environment %s", ErrArchived, id)
			}
		}
	}
	for _, channelID := range scope.ChannelIDs {
		ch, err := c.channels.GetChannel(ctx, organizationID, channelID)
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
func (c *VariableScopeChecks) checkStores(ctx context.Context, v *Variable, workspaceID string, targets map[string]*Target) error {
	ids := v.SecretStoreIDs()
	if len(ids) == 0 {
		return nil
	}
	stores, err := c.stores.LookupSecretStores(ctx, v.OrganizationID(), ids)
	if err != nil {
		return fmt.Errorf("look up secret stores: %w", err)
	}
	for _, value := range v.Values() {
		if value.Secret == nil {
			continue
		}
		store, ok := stores[value.Secret.StoreID]
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
			t, ok := targets[targetID]
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

// lockTargets reads the Targets among IDs FOR UPDATE, in ID order, leaving out
// the ones that do not exist.
func (c *VariableScopeChecks) lockTargets(ctx context.Context, organizationID string, targetIDs []string) (map[string]*Target, error) {
	slices.Sort(targetIDs)
	targetIDs = slices.Compact(targetIDs)
	out := make(map[string]*Target, len(targetIDs))
	for _, targetID := range targetIDs {
		t, err := c.targets.GetTargetForUpdate(ctx, organizationID, targetID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get target %s: %w", targetID, err)
		}
		out[targetID] = t
	}
	return out, nil
}

func sameID(a *string, b string) bool { return a != nil && *a == b }

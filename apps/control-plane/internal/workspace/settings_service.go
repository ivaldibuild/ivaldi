package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// ChangeSettingInput proposes a setting value at one level.
type ChangeSettingInput struct {
	Scope Scope
	Key   string
	Value json.RawMessage
	// ExpectedVersion is the version the caller read: 0 when the level had no
	// value. The change is refused if the stored version differs.
	ExpectedVersion int64
	// ApprovalID resubmits a change that required approval, once approved.
	ApprovalID string
}

// ChangeSetting sets a value at one level. The owning capability's
// definition validates it, the authorizer decides, and the write and its
// audit record commit together. An agent Session's change always needs a
// human's approval (ADR 0007): the first call returns an
// authorization.ApprovalRequiredError, and the change applies when resubmitted with that
// decision's ID after approval.
func (s *Service) ChangeSetting(ctx context.Context, actor identity.Actor, in ChangeSettingInput) (*Setting, error) {
	def, err := s.settingDefinition(in.Scope, in.Key)
	if err != nil {
		return nil, err
	}
	if err := checkValue(in.Value); err != nil {
		return nil, err
	}
	if def.Validate != nil {
		if err := def.Validate(ctx, in.Scope, in.Value); err != nil {
			return nil, fmt.Errorf("%w: setting %s: %w", ErrInvalid, in.Key, err)
		}
	}
	if err := s.checkScopeActive(ctx, in.Scope); err != nil {
		return nil, err
	}
	current, err := s.currentSetting(ctx, in.Scope, in.Key, in.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	var before json.RawMessage
	if current != nil {
		before = current.Value()
	}

	decision, err := s.authorizeSettingChange(ctx, actor, def, SettingChange{
		Scope: in.Scope, Key: in.Key, Level: in.Scope.Level(),
		Before: before, Value: in.Value, ExpectedVersion: in.ExpectedVersion,
	}, ActionSettingChange, in.ApprovalID)
	if err != nil {
		return nil, err
	}

	// The domain object makes the change: a new Setting at version 1, or the
	// current one moved to its next version.
	setting := current
	if setting == nil {
		setting, err = NewSetting(NewSettingProps{
			Scope: in.Scope, Key: in.Key, Value: in.Value,
			UpdatedByPrincipalID: actor.PrincipalID, PolicyDecisionID: decision.ID,
		})
	} else {
		err = setting.Change(in.Value, actor.PrincipalID, decision.ID)
	}
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if current == nil {
			if err := s.repo.InsertSetting(ctx, setting); err != nil {
				return fmt.Errorf("insert setting %s: %w", in.Key, err)
			}
		} else if err := s.repo.UpdateSetting(ctx, setting, in.ExpectedVersion); err != nil {
			return fmt.Errorf("update setting %s: %w", in.Key, err)
		}
		return s.record(ctx, audit.Event{
			Type: EventSettingChanged, OrganizationID: in.Scope.OrganizationID, ResourceID: in.Scope.ResourceID(),
			Actor: actor, DecisionID: decision.ID, ApprovedByPrincipalID: decision.ApprovedByPrincipalID,
			Before: settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: before},
			After:  settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: setting.Value()},
		})
	})
	if err != nil {
		return nil, err
	}
	return setting, nil
}

// RemoveSettingInput removes the value stored at one level, so the value
// from the next level up applies again.
type RemoveSettingInput struct {
	Scope           Scope
	Key             string
	ExpectedVersion int64
	ApprovalID      string
}

// RemoveSetting removes a value at one level. It is authorized like a change,
// so an agent Session needs a human's approval here too.
func (s *Service) RemoveSetting(ctx context.Context, actor identity.Actor, in RemoveSettingInput) error {
	def, err := s.settingDefinition(in.Scope, in.Key)
	if err != nil {
		return err
	}
	if err := s.checkScopeActive(ctx, in.Scope); err != nil {
		return err
	}
	current, err := s.currentSetting(ctx, in.Scope, in.Key, in.ExpectedVersion)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("setting %s at %s level: %w", in.Key, in.Scope.Level(), ErrNotFound)
	}

	decision, err := s.authorizeSettingChange(ctx, actor, def, SettingChange{
		Scope: in.Scope, Key: in.Key, Level: in.Scope.Level(),
		Before: current.Value(), ExpectedVersion: in.ExpectedVersion,
	}, ActionSettingRemove, in.ApprovalID)
	if err != nil {
		return err
	}

	current.Remove(actor.PrincipalID, decision.ID)
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.DeleteSetting(ctx, in.Scope, in.Key, in.ExpectedVersion); err != nil {
			return fmt.Errorf("delete setting %s: %w", in.Key, err)
		}
		return s.record(ctx, audit.Event{
			Type: EventSettingRemoved, OrganizationID: in.Scope.OrganizationID, ResourceID: in.Scope.ResourceID(),
			Actor: actor, DecisionID: decision.ID, ApprovedByPrincipalID: decision.ApprovedByPrincipalID,
			Before: settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: current.Value()},
		})
	})
}

// ResolveSetting returns a setting's effective value for a scope: the most
// specific stored value, unless an Organization or Workspace lock fixes it at
// a higher level, and otherwise the definition's default. It does not
// authorize; it is for capabilities reading their own configuration.
func (s *Service) ResolveSetting(ctx context.Context, scope Scope, key string) (Resolved, error) {
	if err := scope.validate(); err != nil {
		return Resolved{}, err
	}
	def, ok := s.definitions.Lookup(key)
	if !ok {
		return Resolved{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	stored, err := s.repo.SettingsForResolution(ctx, scope, key)
	if err != nil {
		return Resolved{}, fmt.Errorf("read setting %s: %w", key, err)
	}
	locked, err := s.locks.LockedKeys(ctx, scope.OrganizationID, scope.WorkspaceID)
	if err != nil {
		return Resolved{}, fmt.Errorf("read setting locks: %w", err)
	}
	return resolve(def, stored, locked[key]), nil
}

// ListSettings returns the values stored at exactly one level.
func (s *Service) ListSettings(ctx context.Context, actor identity.Actor, scope Scope) ([]*Setting, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	if _, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: PermissionSettingRead, OrganizationID: scope.OrganizationID,
		ResourceID: scope.ResourceID(), Action: ActionSettingList,
	}); err != nil {
		return nil, err
	}
	return s.repo.ListSettings(ctx, scope)
}

// settingAudit is the before or after state of a setting in an audit record.
type settingAudit struct {
	Key   string          `json:"key"`
	Level Level           `json:"level"`
	Value json.RawMessage `json:"value,omitempty"`
}

func (s *Service) settingDefinition(scope Scope, key string) (Definition, error) {
	if err := scope.validate(); err != nil {
		return Definition{}, err
	}
	def, ok := s.definitions.Lookup(key)
	if !ok {
		return Definition{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
	}
	if !def.allows(scope.Level()) {
		return Definition{}, fmt.Errorf("%w: %s at %s level", ErrLevelNotAllowed, key, scope.Level())
	}
	return def, nil
}

// checkScopeActive refuses settings on archived or mismatched Workspaces and Projects.
func (s *Service) checkScopeActive(ctx context.Context, scope Scope) error {
	if scope.WorkspaceID != "" {
		w, err := s.repo.GetWorkspace(ctx, scope.OrganizationID, scope.WorkspaceID)
		if err != nil {
			return err
		}
		if w.Archived() {
			return fmt.Errorf("settings for workspace %s: %w", w.ID(), ErrArchived)
		}
	}
	if scope.ProjectID != "" {
		p, err := s.repo.GetProject(ctx, scope.OrganizationID, scope.ProjectID)
		if err != nil {
			return err
		}
		if p.WorkspaceID() != scope.WorkspaceID {
			return fmt.Errorf("project %s in workspace %s: %w", p.ID(), scope.WorkspaceID, ErrNotFound)
		}
		if p.Archived() {
			return fmt.Errorf("settings for project %s: %w", p.ID(), ErrArchived)
		}
	}
	return nil
}

// currentSetting returns the stored value, or nil if there is none, and
// refuses a change made against a version that is no longer current.
func (s *Service) currentSetting(ctx context.Context, scope Scope, key string, expectedVersion int64) (*Setting, error) {
	current, err := s.repo.GetSetting(ctx, scope, key)
	if errors.Is(err, ErrNotFound) {
		current, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read setting %s: %w", key, err)
	}
	var version int64
	if current != nil {
		version = current.Version()
	}
	if expectedVersion != version {
		return nil, fmt.Errorf("setting %s is at version %d, not %d: %w", key, version, expectedVersion, ErrConflict)
	}
	return current, nil
}

func (s *Service) authorizeSettingChange(ctx context.Context, actor identity.Actor, def Definition, change SettingChange, action, approvalID string) (authorization.Decision, error) {
	decision, err := s.authorize(ctx, authorization.Request{
		Actor: actor, Permission: def.Permission, OrganizationID: change.Scope.OrganizationID,
		ResourceID: change.Scope.ResourceID(), Action: action, Proposal: change, ApprovalID: approvalID,
	})
	if err != nil {
		return authorization.Decision{}, err
	}
	// Policy enforces that agent Sessions never change settings without a
	// human's approval (ADR 0007). Refuse here too if a decision ever says otherwise.
	if actor.IsAgent() && decision.ApprovedByPrincipalID == "" {
		return authorization.Decision{}, &authorization.DeniedError{DecisionID: decision.ID, Reason: "an agent session's setting change needs a human's approval"}
	}
	return decision, nil
}

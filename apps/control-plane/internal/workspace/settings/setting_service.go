// Package settings implements workspace.SettingService: the setting values
// stored for Organizations, Workspaces, and Projects, and their HTTP API.
// The domain types and the setting definitions stay in workspace; this
// package orchestrates them.
//
// NewSettingService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller (the HTTP
// API, MCP, workers) is checked the same way. The service below assumes that
// check has happened, stores the decision that allowed each change on the
// Setting, and records it in the audit record. Other capabilities read their
// own settings through workspace.SettingResolver, which does not authorize.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type SettingServiceOption func(*options)

type options struct {
	repo        workspace.Repository
	definitions *workspace.Definitions
	locks       workspace.LockSource
	authz       authorization.Authorizer
	writer      change.Writer
}

func WithRepository(r workspace.Repository) SettingServiceOption {
	return func(o *options) { o.repo = r }
}

// WithDefinitions is the registry of the settings capabilities define.
func WithDefinitions(d *workspace.Definitions) SettingServiceOption {
	return func(o *options) { o.definitions = d }
}

// WithLockSource reports the keys policy locks (policy.NewLockSource):
// changes below a lock are refused, and resolution stops at the lock's level.
func WithLockSource(l workspace.LockSource) SettingServiceOption {
	return func(o *options) { o.locks = l }
}

func WithAuthorizer(a authorization.Authorizer) SettingServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) SettingServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) SettingServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) SettingServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewSettingService returns the SettingService, authorizing every
// operation, or an error naming a missing dependency.
func NewSettingService(opts ...SettingServiceOption) (workspace.SettingService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Repository", o.repo != nil},
		{"Definitions", o.definitions != nil},
		{"LockSource", o.locks != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("setting service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("setting service: %w", err)
	}
	resolver, err := workspace.NewSettingResolver(o.repo, o.definitions, o.locks)
	if err != nil {
		return nil, fmt.Errorf("setting service: %w", err)
	}
	s := &service{repo: o.repo, definitions: o.definitions, locks: o.locks, resolver: resolver, w: o.writer}
	return &authorized{next: s, authz: o.authz, repo: o.repo, definitions: o.definitions, locks: o.locks}, nil
}

// service carries out setting operations once they are authorized. It
// checks rules that span records, changes Settings only through their
// methods, and commits each change with its events and audit record.
type service struct {
	repo        workspace.Repository
	definitions *workspace.Definitions
	locks       workspace.LockSource
	resolver    workspace.SettingResolver
	w           change.Writer
}

var _ workspace.SettingService = (*service)(nil)

// settingAudit is the before or after state of a setting in an audit record.
type settingAudit struct {
	Key   string          `json:"key"`
	Level workspace.Level `json:"level"`
	Value json.RawMessage `json:"value,omitempty"`
}

// decision returns the authorizer's decision for a change at scope, which
// the Setting records.
func decision(ctx context.Context, scope workspace.Scope, action string) (authorization.Decision, error) {
	d, ok := authorization.DecisionFor(ctx, scope.ResourceID())
	if !ok {
		return authorization.Decision{}, fmt.Errorf("%w: %s", change.ErrUnauthorized, action)
	}
	return d, nil
}

// ChangeSetting sets a value at one level: a new Setting at version 1, or
// the current one moved to its next version, refused if the version the
// caller read is no longer current or policy locks the key above the level.
func (s *service) ChangeSetting(ctx context.Context, actor identity.Actor, in workspace.ChangeSettingInput) (*workspace.Setting, error) {
	def, err := s.definitions.ForScope(in.Scope, in.Key)
	if err != nil {
		return nil, err
	}
	d, err := decision(ctx, in.Scope, workspace.ActionSettingChange)
	if err != nil {
		return nil, err
	}
	var setting *workspace.Setting
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := def.ValidateValue(ctx, in.Scope, in.Value); err != nil {
			return err
		}
		if err := s.checkScopeActive(ctx, in.Scope); err != nil {
			return err
		}
		if err := workspace.CheckUnlocked(ctx, s.locks, in.Scope, in.Key); err != nil {
			return err
		}
		current, err := currentSetting(ctx, s.repo, in.Scope, in.Key, in.ExpectedVersion)
		if err != nil {
			return err
		}
		var before json.RawMessage
		if current == nil {
			setting, err = workspace.NewSetting(workspace.NewSettingProps{
				Scope: in.Scope, Key: in.Key, Value: in.Value,
				UpdatedByPrincipalID: actor.PrincipalID, PolicyDecisionID: d.ID,
			})
			if err != nil {
				return err
			}
			if err := s.repo.InsertSetting(ctx, setting); err != nil {
				return fmt.Errorf("insert setting %s: %w", in.Key, err)
			}
		} else {
			before = current.Value()
			setting = current
			if err := setting.Change(in.Value, actor.PrincipalID, d.ID); err != nil {
				return err
			}
			if err := s.repo.UpdateSetting(ctx, setting, in.ExpectedVersion); err != nil {
				return fmt.Errorf("update setting %s: %w", in.Key, err)
			}
		}
		if err := s.w.Events(ctx, workspace.AggregateSetting, setting.AggregateID(), setting); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: workspace.EventSettingChanged, OrganizationID: in.Scope.OrganizationID, ResourceID: in.Scope.ResourceID(), Actor: actor,
			Before: settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: before},
			After:  settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: setting.Value()},
		})
	})
	if err != nil {
		return nil, err
	}
	return setting, nil
}

// RemoveSetting removes a value at one level, refused if the version the
// caller read is no longer current or policy locks the key above the level.
// A value below a lock is inactive; removing the lock brings it back, so it
// can only be removed once the lock is.
func (s *service) RemoveSetting(ctx context.Context, actor identity.Actor, in workspace.RemoveSettingInput) error {
	if _, err := s.definitions.ForScope(in.Scope, in.Key); err != nil {
		return err
	}
	d, err := decision(ctx, in.Scope, workspace.ActionSettingRemove)
	if err != nil {
		return err
	}
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.checkScopeActive(ctx, in.Scope); err != nil {
			return err
		}
		if err := workspace.CheckUnlocked(ctx, s.locks, in.Scope, in.Key); err != nil {
			return err
		}
		current, err := currentSetting(ctx, s.repo, in.Scope, in.Key, in.ExpectedVersion)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("setting %s at %s level: %w", in.Key, in.Scope.Level(), workspace.ErrNotFound)
		}
		current.Remove(actor.PrincipalID, d.ID)
		if err := s.repo.DeleteSetting(ctx, in.Scope, in.Key, in.ExpectedVersion); err != nil {
			return fmt.Errorf("delete setting %s: %w", in.Key, err)
		}
		if err := s.w.Events(ctx, workspace.AggregateSetting, current.AggregateID(), current); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: workspace.EventSettingRemoved, OrganizationID: in.Scope.OrganizationID, ResourceID: in.Scope.ResourceID(), Actor: actor,
			Before: settingAudit{Key: in.Key, Level: in.Scope.Level(), Value: current.Value()},
		})
	})
}

func (s *service) ListSettings(ctx context.Context, _ identity.Actor, scope workspace.Scope) ([]*workspace.Setting, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.repo.ListSettings(ctx, scope)
}

func (s *service) ResolveSetting(ctx context.Context, _ identity.Actor, scope workspace.Scope, key string) (workspace.Resolved, error) {
	return s.resolver.ResolveSetting(ctx, scope, key)
}

// checkScopeActive refuses settings on archived or mismatched Workspaces and Projects.
func (s *service) checkScopeActive(ctx context.Context, scope workspace.Scope) error {
	if scope.WorkspaceID != "" {
		w, err := s.repo.GetWorkspace(ctx, scope.OrganizationID, scope.WorkspaceID)
		if err != nil {
			return err
		}
		if w.Archived() {
			return fmt.Errorf("settings for workspace %s: %w", w.ID(), workspace.ErrArchived)
		}
	}
	if scope.ProjectID != "" {
		p, err := s.repo.GetProject(ctx, scope.OrganizationID, scope.ProjectID)
		if err != nil {
			return err
		}
		if p.WorkspaceID() != scope.WorkspaceID {
			return fmt.Errorf("project %s in workspace %s: %w", p.ID(), scope.WorkspaceID, workspace.ErrNotFound)
		}
		if p.Archived() {
			return fmt.Errorf("settings for project %s: %w", p.ID(), workspace.ErrArchived)
		}
	}
	return nil
}

// currentSetting returns the stored value, or nil if there is none, and
// refuses a change made against a version that is no longer current.
func currentSetting(ctx context.Context, repo workspace.Repository, scope workspace.Scope, key string, expectedVersion int64) (*workspace.Setting, error) {
	current, err := repo.GetSetting(ctx, scope, key)
	if errors.Is(err, workspace.ErrNotFound) {
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
		return nil, fmt.Errorf("setting %s is at version %d, not %d: %w", key, version, expectedVersion, workspace.ErrConflict)
	}
	return current, nil
}

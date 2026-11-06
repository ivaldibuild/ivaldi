// Package locks implements policy.SettingLockService: the setting locks
// Organizations and Workspaces set, and their HTTP API. The domain types
// stay in policy; this package orchestrates them.
//
// NewSettingLockService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and
// records the decision that allowed each change in its audit record.
package locks

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type SettingLockServiceOption func(*options)

type options struct {
	locks       policy.SettingLockRepository
	definitions policy.SettingDefinitions
	workspaces  policy.Workspaces
	authz       authorization.Authorizer
	writer      change.Writer
}

func WithSettingLockRepo(r policy.SettingLockRepository) SettingLockServiceOption {
	return func(o *options) { o.locks = r }
}

// WithDefinitions is the registry of the settings capabilities define
// (*workspace.Definitions): only a registered setting that can be set below
// a lock's level can be locked.
func WithDefinitions(d policy.SettingDefinitions) SettingLockServiceOption {
	return func(o *options) { o.definitions = d }
}

// WithWorkspaces checks that a Workspace lock names an active Workspace
// (workspace.NewLookups).
func WithWorkspaces(w policy.Workspaces) SettingLockServiceOption {
	return func(o *options) { o.workspaces = w }
}

func WithAuthorizer(a authorization.Authorizer) SettingLockServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) SettingLockServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) SettingLockServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) SettingLockServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewSettingLockService returns the SettingLockService, authorizing every
// operation, or an error naming a missing dependency.
func NewSettingLockService(opts ...SettingLockServiceOption) (policy.SettingLockService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"SettingLockRepo", o.locks != nil},
		{"Definitions", o.definitions != nil},
		{"Workspaces", o.workspaces != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("setting lock service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("setting lock service: %w", err)
	}
	s := &service{locks: o.locks, definitions: o.definitions, workspaces: o.workspaces, w: o.writer}
	return &authorized{next: s, authz: o.authz, definitions: o.definitions}, nil
}

// service carries out setting lock operations once they are authorized. It
// checks rules that span records, creates and removes locks only through
// the domain, and commits each change with its events and audit record.
type service struct {
	locks       policy.SettingLockRepository
	definitions policy.SettingDefinitions
	workspaces  policy.Workspaces
	w           change.Writer
}

var _ policy.SettingLockService = (*service)(nil)

func (s *service) ListSettingLocks(ctx context.Context, _ identity.Actor, scope policy.LockScope) ([]*policy.SettingLock, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.locks.ListSettingLocks(ctx, scope)
}

// AddSettingLock locks a key at an Organization or an active Workspace. If
// the key is already locked there, it returns that lock and records nothing.
func (s *service) AddSettingLock(ctx context.Context, actor identity.Actor, in policy.SettingLockInput) (*policy.SettingLock, error) {
	if err := policy.ValidateLockKey(s.definitions, in.Scope, in.Key); err != nil {
		return nil, err
	}
	var lock *policy.SettingLock
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.activeWorkspace(ctx, in.Scope); err != nil {
			return err
		}
		existing, err := s.locks.GetSettingLock(ctx, in.Scope, in.Key)
		if err == nil {
			lock = existing
			return nil
		}
		if !errors.Is(err, policy.ErrNotFound) {
			return fmt.Errorf("read setting lock %s: %w", in.Key, err)
		}
		lock, err = policy.NewSettingLock(policy.NewSettingLockProps{Scope: in.Scope, Key: in.Key, LockedByPrincipalID: actor.PrincipalID}, nil)
		if err != nil {
			return err
		}
		if err := s.locks.CreateSettingLock(ctx, lock); err != nil {
			return fmt.Errorf("create setting lock %s: %w", in.Key, err)
		}
		if err := s.w.Events(ctx, policy.AggregateSettingLock, lock.ID(), lock); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: policy.EventSettingLockAdded, OrganizationID: lock.OrganizationID(), ResourceID: lock.ID(), Actor: actor, After: lock.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return lock, nil
}

// RemoveSettingLock removes a lock, so the values below its level apply
// again. The row is deleted; its audit record and removed event keep it.
func (s *service) RemoveSettingLock(ctx context.Context, actor identity.Actor, in policy.SettingLockInput) error {
	if err := in.Scope.Validate(); err != nil {
		return err
	}
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		lock, err := s.locks.GetSettingLock(ctx, in.Scope, in.Key)
		if err != nil {
			return fmt.Errorf("setting lock %s at %s level: %w", in.Key, in.Scope.Level(), err)
		}
		lock.Remove(actor.PrincipalID)
		if err := s.locks.DeleteSettingLock(ctx, lock.OrganizationID(), lock.ID()); err != nil {
			return fmt.Errorf("delete setting lock %s: %w", in.Key, err)
		}
		if err := s.w.Events(ctx, policy.AggregateSettingLock, lock.ID(), lock); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: policy.EventSettingLockRemoved, OrganizationID: lock.OrganizationID(), ResourceID: lock.ID(), Actor: actor, Before: lock.Snapshot(),
		})
	})
}

// activeWorkspace checks that a Workspace lock names an active Workspace.
func (s *service) activeWorkspace(ctx context.Context, scope policy.LockScope) error {
	if scope.WorkspaceID == "" {
		return nil
	}
	w, err := s.workspaces.LookupWorkspace(ctx, scope.OrganizationID, scope.WorkspaceID)
	if errors.Is(err, workspace.ErrNotFound) {
		return fmt.Errorf("%w: workspace %s", policy.ErrNotFound, scope.WorkspaceID)
	}
	if err != nil {
		return fmt.Errorf("look up workspace %s: %w", scope.WorkspaceID, err)
	}
	if w.Archived() {
		return fmt.Errorf("%w: workspace %s", policy.ErrArchived, scope.WorkspaceID)
	}
	return nil
}

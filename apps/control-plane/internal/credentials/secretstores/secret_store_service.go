// Package secretstores implements credentials.SecretStoreService: a
// Workspace's Secret Stores and their HTTP API. The domain types stay in
// credentials; this package orchestrates them.
//
// NewSecretStoreService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. The service below assumes
// that check has happened and records the decision that allowed each change
// in its audit record.
package secretstores

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Workspaces looks up the Workspace a store belongs to, without
// authorizing. workspace.NewLookups provides it.
type Workspaces interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error)
}

// EnvironmentLookup reads the Environments a store names, to check they are
// active and in its Workspace. delivery.EnvironmentRepository provides it.
type EnvironmentLookup interface {
	GetEnvironments(ctx context.Context, organizationID string, environmentIDs []string) ([]*delivery.Environment, error)
}

// VariableUsage counts the active variables with a value that refers to a
// store, which keep it from being archived. delivery.VariableRepository
// provides it.
type VariableUsage interface {
	CountActiveVariablesWithSecretStore(ctx context.Context, organizationID, storeID string) (int, error)
}

type SecretStoreServiceOption func(*options)

type options struct {
	stores       credentials.SecretStoreRepository
	workspaces   Workspaces
	environments EnvironmentLookup
	variables    VariableUsage
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithSecretStoreRepo(r credentials.SecretStoreRepository) SecretStoreServiceOption {
	return func(o *options) { o.stores = r }
}
func WithWorkspaces(w Workspaces) SecretStoreServiceOption {
	return func(o *options) { o.workspaces = w }
}
func WithEnvironmentLookup(e EnvironmentLookup) SecretStoreServiceOption {
	return func(o *options) { o.environments = e }
}
func WithVariableUsage(v VariableUsage) SecretStoreServiceOption {
	return func(o *options) { o.variables = v }
}
func WithAuthorizer(a authorization.Authorizer) SecretStoreServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) SecretStoreServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) SecretStoreServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) SecretStoreServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewSecretStoreService returns the SecretStoreService, authorizing every
// operation, or an error naming a missing dependency.
func NewSecretStoreService(opts ...SecretStoreServiceOption) (credentials.SecretStoreService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"SecretStoreRepo", o.stores != nil},
		{"Workspaces", o.workspaces != nil},
		{"EnvironmentLookup", o.environments != nil},
		{"VariableUsage", o.variables != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("secret store service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("secret store service: %w", err)
	}
	s := &service{stores: o.stores, workspaces: o.workspaces, environments: o.environments, variables: o.variables, w: o.writer}
	return &authorized{next: s, authz: o.authz, stores: o.stores}, nil
}

// service carries out Secret Store operations once they are authorized.
type service struct {
	stores       credentials.SecretStoreRepository
	workspaces   Workspaces
	environments EnvironmentLookup
	variables    VariableUsage
	w            change.Writer
}

var _ credentials.SecretStoreService = (*service)(nil)

// CreateSecretStore creates a Secret Store in an active Workspace. It may
// name only that Workspace's active Environments.
func (s *service) CreateSecretStore(ctx context.Context, actor identity.Actor, in credentials.CreateSecretStoreInput) (*credentials.SecretStore, error) {
	store, err := credentials.NewSecretStore(credentials.NewSecretStoreProps{
		OrganizationID: in.OrganizationID, WorkspaceID: in.WorkspaceID, Slug: in.Slug, DisplayName: in.DisplayName,
		Kind: in.Kind, Settings: in.Settings, EnvironmentIDs: in.EnvironmentIDs,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		w, err := s.workspaces.LookupWorkspace(ctx, in.OrganizationID, in.WorkspaceID)
		if errors.Is(err, workspace.ErrNotFound) {
			return fmt.Errorf("%w: workspace %s", credentials.ErrNotFound, in.WorkspaceID)
		}
		if err != nil {
			return fmt.Errorf("look up workspace %s: %w", in.WorkspaceID, err)
		}
		if w.Archived() {
			return fmt.Errorf("%w: workspace %s", credentials.ErrArchived, in.WorkspaceID)
		}
		n, err := s.stores.CountActiveSecretStores(ctx, in.OrganizationID, in.WorkspaceID)
		if err != nil {
			return fmt.Errorf("count secret stores: %w", err)
		}
		if n >= credentials.MaxSecretStoresPerWorkspace {
			return credentials.NewErrInvalidInput(fmt.Sprintf("a workspace can have at most %d secret stores", credentials.MaxSecretStoresPerWorkspace))
		}
		if err := s.checkEnvironments(ctx, store); err != nil {
			return err
		}
		if err := s.stores.CreateSecretStore(ctx, store); err != nil {
			return fmt.Errorf("create secret store: %w", err)
		}
		if err := s.w.Events(ctx, credentials.AggregateSecretStore, store.ID(), store); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: credentials.EventSecretStoreCreated, OrganizationID: in.OrganizationID, ResourceID: store.ID(), Actor: actor, After: store.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return store, nil
}

// checkEnvironments checks that every Environment the store names exists,
// is active, and is in the store's Workspace.
func (s *service) checkEnvironments(ctx context.Context, store *credentials.SecretStore) error {
	ids := store.EnvironmentIDs()
	if len(ids) == 0 {
		return nil
	}
	envs, err := s.environments.GetEnvironments(ctx, store.OrganizationID(), ids)
	if err != nil {
		return fmt.Errorf("get environments: %w", err)
	}
	byID := make(map[string]*delivery.Environment, len(envs))
	for _, e := range envs {
		byID[e.ID()] = e
	}
	for _, id := range ids {
		e, ok := byID[id]
		switch {
		case !ok:
			return credentials.NewErrInvalidInput(fmt.Sprintf("environment %s does not exist", id))
		case e.WorkspaceID() != store.WorkspaceID():
			return fmt.Errorf("%w: environment %s is not in workspace %s", credentials.ErrWorkspaceMismatch, id, store.WorkspaceID())
		case e.Archived():
			return fmt.Errorf("%w: environment %s", credentials.ErrArchived, id)
		}
	}
	return nil
}

func (s *service) GetSecretStore(ctx context.Context, _ identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
	return s.stores.GetSecretStore(ctx, organizationID, storeID)
}

func (s *service) ListSecretStores(ctx context.Context, _ identity.Actor, organizationID, workspaceID string, includeArchived bool) ([]*credentials.SecretStore, error) {
	return s.stores.ListSecretStores(ctx, organizationID, workspaceID, includeArchived)
}

func (s *service) changeStore(ctx context.Context, actor identity.Actor, organizationID, storeID, event string, apply func(context.Context, *credentials.SecretStore) error) (*credentials.SecretStore, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*credentials.SecretStore]{
		AggregateType: credentials.AggregateSecretStore,
		AggregateID:   func(store *credentials.SecretStore) string { return store.ID() },
		// Locked, because archiving a store counts the values kept in it and
		// a save of one holds the store's row FOR SHARE: whichever runs
		// first, the other sees it.
		Load: func(ctx context.Context) (*credentials.SecretStore, error) {
			return s.stores.GetSecretStoreForUpdate(ctx, organizationID, storeID)
		},
		Apply:    apply,
		Save:     s.stores.UpdateSecretStore,
		Snapshot: func(store *credentials.SecretStore) any { return store.Snapshot() },
		Event: func(store *credentials.SecretStore) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: store.ID()}
		},
	})
}

// UpdateSecretStore changes an active store's name, slug, settings, or the
// Environments that may read it.
func (s *service) UpdateSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string, props credentials.UpdateSecretStoreProps) (*credentials.SecretStore, error) {
	return s.changeStore(ctx, actor, organizationID, storeID, credentials.EventSecretStoreUpdated,
		func(ctx context.Context, store *credentials.SecretStore) error {
			if err := store.Update(props); err != nil {
				return err
			}
			return s.checkEnvironments(ctx, store)
		})
}

// ArchiveSecretStore archives a store no active variable's value refers to.
// Archiving an archived one does nothing.
func (s *service) ArchiveSecretStore(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
	return s.changeStore(ctx, actor, organizationID, storeID, credentials.EventSecretStoreArchived,
		func(ctx context.Context, store *credentials.SecretStore) error {
			if store.Archived() {
				return change.ErrUnchanged
			}
			n, err := s.variables.CountActiveVariablesWithSecretStore(ctx, organizationID, store.ID())
			if err != nil {
				return fmt.Errorf("count variables: %w", err)
			}
			if n > 0 {
				return fmt.Errorf("%w: %d variables have a value in secret store %s", credentials.ErrInUse, n, store.ID())
			}
			store.Archive()
			return nil
		})
}

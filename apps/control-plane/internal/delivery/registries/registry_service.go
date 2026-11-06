// Package registries implements delivery.RegistryService: the Registries a
// Workspace's artifacts are looked up in, and their HTTP API. The domain
// types stay in delivery; this package orchestrates them.
//
// NewRegistryService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and
// records the decision that allowed each change in its audit record.
package registries

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type RegistryServiceOption func(*options)

type options struct {
	registries delivery.RegistryRepository
	workspaces delivery.Workspaces
	authz      authorization.Authorizer
	writer     change.Writer
}

func WithRegistryRepo(r delivery.RegistryRepository) RegistryServiceOption {
	return func(o *options) { o.registries = r }
}
func WithWorkspaces(w delivery.Workspaces) RegistryServiceOption {
	return func(o *options) { o.workspaces = w }
}
func WithAuthorizer(a authorization.Authorizer) RegistryServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must write
// through its context.
func WithTransactor(tx transaction.Transactor) RegistryServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) RegistryServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) RegistryServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewRegistryService returns the RegistryService, authorizing every
// operation, or an error naming a missing dependency.
func NewRegistryService(opts ...RegistryServiceOption) (delivery.RegistryService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"RegistryRepo", o.registries != nil},
		{"Workspaces", o.workspaces != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("registry service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("registry service: %w", err)
	}
	s := &service{registries: o.registries, workspaces: o.workspaces, w: o.writer}
	return &authorized{next: s, authz: o.authz, registries: o.registries}, nil
}

// service carries out Registry operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	registries delivery.RegistryRepository
	workspaces delivery.Workspaces
	w          change.Writer
}

var _ delivery.RegistryService = (*service)(nil)

// CreateRegistry creates a Registry in an active Workspace.
func (s *service) CreateRegistry(ctx context.Context, actor identity.Actor, in delivery.CreateRegistryInput) (*delivery.Registry, error) {
	r, err := delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: in.OrganizationID,
		WorkspaceID:    in.WorkspaceID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Kind:           in.Kind,
		URL:            in.URL,
		CredentialRef:  &in.CredentialRef,
		LookupFrom:     in.LookupFrom,
		LookupPool:     &in.LookupPool,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := delivery.ActiveWorkspace(ctx, s.workspaces, r.OrganizationID(), r.WorkspaceID()); err != nil {
			return err
		}
		if err := s.registries.CreateRegistry(ctx, r); err != nil {
			return fmt.Errorf("create registry: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateRegistry, r.ID(), r); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventRegistryCreated, OrganizationID: r.OrganizationID(), ResourceID: r.ID(), Actor: actor, After: r.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *service) GetRegistry(ctx context.Context, _ identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	return s.registries.GetRegistry(ctx, organizationID, registryID)
}

func (s *service) ListRegistries(ctx context.Context, _ identity.Actor, query delivery.ListRegistriesQuery) (*pagination.Result[*delivery.Registry], error) {
	return s.registries.ListRegistries(ctx, query)
}

func (s *service) changeRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID, event string, apply func(context.Context, *delivery.Registry) error) (*delivery.Registry, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Registry]{
		AggregateType: delivery.AggregateRegistry,
		AggregateID:   func(r *delivery.Registry) string { return r.ID() },
		Load: func(ctx context.Context) (*delivery.Registry, error) {
			return s.registries.GetRegistry(ctx, organizationID, registryID)
		},
		Apply:    apply,
		Save:     s.registries.UpdateRegistry,
		Snapshot: func(r *delivery.Registry) any { return r.Snapshot() },
		Event: func(r *delivery.Registry) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: r.ID()}
		},
	})
}

// UpdateRegistry changes an active Registry's slug, name, or URL.
func (s *service) UpdateRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string, props delivery.UpdateRegistryProps) (*delivery.Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, delivery.EventRegistryUpdated,
		func(_ context.Context, r *delivery.Registry) error { return r.Update(props) })
}

// SetRegistryCredential points the Registry at a credential, or makes it
// anonymous when credentialRef is empty.
func (s *service) SetRegistryCredential(ctx context.Context, actor identity.Actor, organizationID, registryID, credentialRef string) (*delivery.Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, delivery.EventRegistryUpdated,
		func(_ context.Context, r *delivery.Registry) error {
			if credentialRef == "" {
				return r.ClearCredential()
			}
			return r.SetCredential(credentialRef)
		})
}

// SetRegistryLookup changes where lookups against the Registry run.
func (s *service) SetRegistryLookup(ctx context.Context, actor identity.Actor, organizationID, registryID string, from delivery.LookupFrom, pool string) (*delivery.Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, delivery.EventRegistryUpdated,
		func(_ context.Context, r *delivery.Registry) error { return r.SetLookup(from, pool) })
}

// ArchiveRegistry archives a Registry. Artifacts that use it keep the
// reference, so their Builds can still be looked up once it is restored.
// Archiving an archived one does nothing.
func (s *service) ArchiveRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, delivery.EventRegistryArchived,
		func(_ context.Context, r *delivery.Registry) error {
			if r.Archived() {
				return change.ErrUnchanged
			}
			r.Archive()
			return nil
		})
}

// RestoreRegistry restores an archived Registry in an active Workspace.
func (s *service) RestoreRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
	return s.changeRegistry(ctx, actor, organizationID, registryID, delivery.EventRegistryRestored,
		func(ctx context.Context, r *delivery.Registry) error {
			if !r.Archived() {
				return change.ErrUnchanged
			}
			if err := delivery.ActiveWorkspace(ctx, s.workspaces, organizationID, r.WorkspaceID()); err != nil {
				return err
			}
			r.Restore()
			return nil
		})
}

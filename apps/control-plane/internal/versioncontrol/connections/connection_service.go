// Package connections implements versioncontrol.ConnectionService: an
// Organization's provider Connections and their HTTP API.
//
// NewConnectionService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. The service below assumes
// that check has happened and records the decision that allowed each change
// in its audit record.
package connections

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type Option func(*options)

type options struct {
	connections versioncontrol.ConnectionRepository
	authz       authorization.Authorizer
	writer      change.Writer
	installers  map[vcs.ProviderKind]vcs.Installer
	states      versioncontrol.StateSealer
	provider    vcs.Provider
	connected   versioncontrol.ConnectedRepositories
}

func WithConnectionRepo(r versioncontrol.ConnectionRepository) Option {
	return func(o *options) { o.connections = r }
}

// WithProvider lists a Connection's repositories. It is optional: without
// one, listing fails with a clear error.
func WithProvider(p vcs.Provider) Option {
	return func(o *options) { o.provider = p }
}

// WithConnectedRepositories marks the listed repositories that an active
// Source already connects.
func WithConnectedRepositories(c versioncontrol.ConnectedRepositories) Option {
	return func(o *options) { o.connected = c }
}

// WithInstaller connects the provider's accounts through its install flow.
// Without one, installing for that provider fails with a clear error.
func WithInstaller(provider vcs.ProviderKind, installer vcs.Installer) Option {
	return func(o *options) {
		if installer == nil {
			return
		}
		if o.installers == nil {
			o.installers = map[vcs.ProviderKind]vcs.Installer{}
		}
		o.installers[provider] = installer
	}
}

// WithStateSealer seals the state an install flow carries through the
// provider. It is required with an installer.
func WithStateSealer(s versioncontrol.StateSealer) Option {
	return func(o *options) { o.states = s }
}

func WithAuthorizer(a authorization.Authorizer) Option {
	return func(o *options) { o.authz = a }
}

func WithTransactor(tx transaction.Transactor) Option {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) Option {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) Option {
	return func(o *options) { o.writer.Audit = r }
}

// NewConnectionService returns the ConnectionService, authorizing every
// operation, or an error naming a missing dependency.
func NewConnectionService(opts ...Option) (versioncontrol.ConnectionService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.connections == nil:
		return nil, fmt.Errorf("connection service: ConnectionRepo is required")
	case o.authz == nil:
		return nil, fmt.Errorf("connection service: Authorizer is required")
	case o.connected == nil:
		return nil, fmt.Errorf("connection service: ConnectedRepositories is required")
	case len(o.installers) > 0 && o.states == nil:
		return nil, fmt.Errorf("connection service: StateSealer is required with an installer")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("connection service: %w", err)
	}
	s := &service{connections: o.connections, w: o.writer, installers: o.installers, states: o.states, provider: o.provider, connected: o.connected}
	return &authorized{next: s, authz: o.authz}, nil
}

type service struct {
	connections versioncontrol.ConnectionRepository
	w           change.Writer
	installers  map[vcs.ProviderKind]vcs.Installer
	states      versioncontrol.StateSealer
	provider    vcs.Provider
	connected   versioncontrol.ConnectedRepositories
}

var _ versioncontrol.ConnectionService = (*service)(nil)

func (s *service) CreateConnection(ctx context.Context, actor identity.Actor, in versioncontrol.CreateConnectionInput) (*versioncontrol.Connection, error) {
	// An app installation's ID proves nothing about who may use it, so it
	// is connected only through the install flow.
	if in.AuthKind == versioncontrol.AuthKindAppInstallation {
		return nil, versioncontrol.NewErrInvalidInput("connect an app installation through the install flow, not by its ID")
	}
	c, err := versioncontrol.NewConnection(versioncontrol.NewConnectionProps{
		OrganizationID:       in.OrganizationID,
		Provider:             in.Provider,
		HostURL:              in.HostURL,
		AuthKind:             in.AuthKind,
		ExternalAccountID:    in.ExternalAccountID,
		ExternalAccountName:  in.ExternalAccountName,
		LookupFrom:           in.LookupFrom,
		LookupPool:           in.LookupPool,
		Status:               versioncontrol.StatusActive,
		CreatedByPrincipalID: &actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := s.create(ctx, actor, c); err != nil {
		return nil, err
	}
	return c, nil
}

// create saves a new Connection with its events and audit record.
func (s *service) create(ctx context.Context, actor identity.Actor, c *versioncontrol.Connection) error {
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.connections.CreateConnection(ctx, c); err != nil {
			return fmt.Errorf("create connection: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateConnection, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventConnectionCreated, OrganizationID: c.OrganizationID(),
			ResourceID: c.ID(), Actor: actor, After: c.Snapshot(),
		})
	})
}

func (s *service) GetConnection(ctx context.Context, _ identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return s.connections.GetConnection(ctx, organizationID, connectionID)
}

func (s *service) ListConnections(ctx context.Context, _ identity.Actor, organizationID string, includeArchived bool) ([]*versioncontrol.Connection, error) {
	return s.connections.ListConnections(ctx, organizationID, includeArchived)
}

func (s *service) ArchiveConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	var out *versioncontrol.Connection
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := s.connections.GetConnectionForUpdate(ctx, organizationID, connectionID)
		if err != nil {
			return err
		}
		if c.Archived() {
			out = c
			return nil
		}
		before := c.Snapshot()
		c.Archive()
		if err := s.connections.UpdateConnection(ctx, c); err != nil {
			return fmt.Errorf("archive connection: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateConnection, c.ID(), c); err != nil {
			return err
		}
		if err := s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventConnectionArchived, OrganizationID: organizationID,
			ResourceID: c.ID(), Actor: actor, Before: before, After: c.Snapshot(),
		}); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) SuspendConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return s.changeStatus(ctx, actor, organizationID, connectionID, versioncontrol.StatusSuspended,
		versioncontrol.EventConnectionSuspended, (*versioncontrol.Connection).Suspend)
}

func (s *service) RevokeConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return s.changeStatus(ctx, actor, organizationID, connectionID, versioncontrol.StatusRevoked,
		versioncontrol.EventConnectionRevoked, (*versioncontrol.Connection).Revoke)
}

func (s *service) ReactivateConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	return s.changeStatus(ctx, actor, organizationID, connectionID, versioncontrol.StatusActive,
		versioncontrol.EventConnectionReactivated, (*versioncontrol.Connection).Reactivate)
}

// changeStatus moves a Connection to status through apply, saving and
// recording nothing when it is already there.
func (s *service) changeStatus(ctx context.Context, actor identity.Actor, organizationID, connectionID string, status versioncontrol.Status, event string, apply func(*versioncontrol.Connection) error) (*versioncontrol.Connection, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*versioncontrol.Connection]{
		AggregateType: versioncontrol.AggregateConnection,
		AggregateID:   func(c *versioncontrol.Connection) string { return c.ID() },
		Load: func(ctx context.Context) (*versioncontrol.Connection, error) {
			return s.connections.GetConnectionForUpdate(ctx, organizationID, connectionID)
		},
		Apply: func(_ context.Context, c *versioncontrol.Connection) error {
			if c.Status() == status {
				return change.ErrUnchanged
			}
			return apply(c)
		},
		Save:     s.connections.UpdateConnection,
		Snapshot: func(c *versioncontrol.Connection) any { return c.Snapshot() },
		Event: func(c *versioncontrol.Connection) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: c.ID()}
		},
	})
}

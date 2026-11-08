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
)

type Option func(*options)

type options struct {
	connections versioncontrol.ConnectionRepository
	authz       authorization.Authorizer
	writer      change.Writer
}

func WithConnectionRepo(r versioncontrol.ConnectionRepository) Option {
	return func(o *options) { o.connections = r }
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
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("connection service: %w", err)
	}
	s := &service{connections: o.connections, w: o.writer}
	return &authorized{next: s, authz: o.authz}, nil
}

type service struct {
	connections versioncontrol.ConnectionRepository
	w           change.Writer
}

var _ versioncontrol.ConnectionService = (*service)(nil)

func (s *service) CreateConnection(ctx context.Context, actor identity.Actor, in versioncontrol.CreateConnectionInput) (*versioncontrol.Connection, error) {
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
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
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
	if err != nil {
		return nil, err
	}
	return c, nil
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

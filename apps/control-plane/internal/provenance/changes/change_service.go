// Package changes implements provenance.ChangeService: recording and reading
// the Changes that tie commits to Sessions and Work Items, and their HTTP
// API.
//
// NewChangeService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. The service below assumes
// that check has happened and records the decision that allowed each change
// in its audit record.
package changes

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type Option func(*options)

type options struct {
	changes provenance.ChangeRepository
	authz   authorization.Authorizer
	writer  change.Writer
}

func WithChangeRepo(r provenance.ChangeRepository) Option {
	return func(o *options) { o.changes = r }
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

// NewChangeService returns the ChangeService, authorizing every operation,
// or an error naming a missing dependency.
func NewChangeService(opts ...Option) (provenance.ChangeService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.changes == nil:
		return nil, fmt.Errorf("change service: ChangeRepo is required")
	case o.authz == nil:
		return nil, fmt.Errorf("change service: Authorizer is required")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("change service: %w", err)
	}
	s := &service{changes: o.changes, w: o.writer}
	return &authorized{next: s, authz: o.authz}, nil
}

type service struct {
	changes provenance.ChangeRepository
	w       change.Writer
}

var _ provenance.ChangeService = (*service)(nil)

func (s *service) RecordChange(ctx context.Context, actor identity.Actor, in provenance.RecordChangeInput) (*provenance.Change, error) {
	c, err := provenance.NewChange(provenance.NewChangeProps{
		OrganizationID:       in.OrganizationID,
		SourceID:             in.SourceID,
		SessionID:            in.SessionID,
		WorkItemID:           in.WorkItemID,
		Title:                in.Title,
		ExternalRef:          in.ExternalRef,
		CommitSHAs:           in.CommitSHAs,
		CreatedByPrincipalID: &actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.changes.CreateChange(ctx, c); err != nil {
			return fmt.Errorf("create change: %w", err)
		}
		if err := s.w.Events(ctx, provenance.AggregateChange, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: provenance.EventChangeRecorded, OrganizationID: c.OrganizationID(),
			ResourceID: c.ID(), Actor: actor, After: c.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *service) GetChange(ctx context.Context, _ identity.Actor, organizationID, changeID string) (*provenance.Change, error) {
	return s.changes.GetChange(ctx, organizationID, changeID)
}

func (s *service) ListChanges(ctx context.Context, _ identity.Actor, query provenance.ListChangesQuery) (*pagination.Result[*provenance.Change], error) {
	return s.changes.ListChanges(ctx, query)
}

func (s *service) ChangesForCommits(ctx context.Context, organizationID, sourceID string, commitSHAs []string) ([]*provenance.Change, error) {
	return s.changes.ChangesForCommits(ctx, organizationID, sourceID, commitSHAs)
}

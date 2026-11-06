// Package organizations reads and changes the caller's Organization:
// GET and PATCH /v1/organization.
package organizations

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type OrganizationServiceOption func(*options)

type options struct {
	orgs   iam.OrganizationRepository
	authz  authorization.Authorizer
	writer change.Writer
}

func WithOrganizationRepo(r iam.OrganizationRepository) OrganizationServiceOption {
	return func(o *options) { o.orgs = r }
}

func WithAuthorizer(a authorization.Authorizer) OrganizationServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor, WithOutboxRecorder, and WithAuditRecorder commit each
// change with its events and audit record.
func WithTransactor(tx transaction.Transactor) OrganizationServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) OrganizationServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) OrganizationServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewOrganizationService returns the OrganizationService, which asks the
// authorizer before every operation. Every option is required.
func NewOrganizationService(opts ...OrganizationServiceOption) (iam.OrganizationService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.orgs == nil:
		return nil, fmt.Errorf("organization service: OrganizationRepo is required")
	case o.authz == nil:
		return nil, fmt.Errorf("organization service: Authorizer is required")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("organization service: %w", err)
	}
	return &authorized{next: &service{orgs: o.orgs, w: o.writer}, authz: o.authz}, nil
}

// service carries out Organization operations once they are authorized. It
// changes the Organization only through its methods, and commits each
// change with its events and audit record.
type service struct {
	orgs iam.OrganizationRepository
	w    change.Writer
}

func (s *service) GetOrganization(ctx context.Context, _ identity.Actor, organizationID string) (*iam.Organization, error) {
	return s.orgs.GetByIdentifier(ctx, organizationID)
}

func (s *service) UpdateOrganization(ctx context.Context, actor identity.Actor, organizationID string, props iam.UpdateOrganizationProps) (*iam.Organization, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*iam.Organization]{
		AggregateType: iam.AggregateOrganization,
		AggregateID:   func(o *iam.Organization) string { return o.ID() },
		Load: func(ctx context.Context) (*iam.Organization, error) {
			return s.orgs.GetForUpdate(ctx, organizationID)
		},
		Apply:    func(_ context.Context, o *iam.Organization) error { return o.Update(props) },
		Save:     s.orgs.Write,
		Snapshot: func(o *iam.Organization) any { return o.Snapshot() },
		Event: func(o *iam.Organization) audit.Event {
			return audit.Event{Type: iam.EventOrganizationUpdated, OrganizationID: o.ID(), ResourceID: o.ID()}
		},
	})
}

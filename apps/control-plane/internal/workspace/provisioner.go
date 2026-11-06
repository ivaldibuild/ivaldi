package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// NewProvisioner returns the iam.OrganizationProvisioner that gives every
// new Organization its default Workspace. IAM calls it inside the
// transaction that creates the Organization, as the system rather than an
// actor, so it asks no authorizer and its audit record carries no decision.
//
// tx must join a transaction already in the context, as
// database.Transactor does, and the repository, the resource registrar, and
// the recorders must write through the context it passes, so the Workspace
// commits or rolls back with the Organization.
func NewProvisioner(repo Repository, tx transaction.Transactor, resources iam.ResourceRegistrar, events outbox.Recorder, records audit.Recorder) (iam.OrganizationProvisioner, error) {
	w := change.Writer{Tx: tx, Outbox: events, Audit: records}
	switch {
	case repo == nil:
		return nil, errors.New("workspace provisioner: a Repository is required")
	case resources == nil:
		return nil, errors.New("workspace provisioner: a ResourceRegistrar is required")
	}
	if err := w.Check(); err != nil {
		return nil, fmt.Errorf("workspace provisioner: %w", err)
	}
	return &provisioner{repo: repo, resources: resources, w: w}, nil
}

type provisioner struct {
	repo      Repository
	resources iam.ResourceRegistrar
	w         change.Writer
}

// ProvisionOrganization creates the Organization's default Workspace,
// registers its IAM resource under the Organization, and records its event
// and audit record, so an Organization never exists without its default
// Workspace.
func (p *provisioner) ProvisionOrganization(ctx context.Context, organizationID string) error {
	w, err := NewDefaultWorkspace(organizationID)
	if err != nil {
		return err
	}
	return p.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := p.repo.CreateWorkspace(ctx, w); err != nil {
			return fmt.Errorf("create default workspace: %w", err)
		}
		if err := p.w.Events(ctx, AggregateWorkspace, w.ID(), w); err != nil {
			return err
		}
		if err := p.resources.RegisterResource(ctx, iam.Resource{ID: w.ID(), OrganizationID: organizationID, Type: iam.ResourceWorkspace, ParentID: organizationID}); err != nil {
			return fmt.Errorf("register workspace resource: %w", err)
		}
		// Written directly rather than through change.Writer.AuditRecord,
		// which needs an authorizer's decision: the system acts here.
		e := audit.Event{
			Type: EventWorkspaceCreated, OrganizationID: organizationID, ResourceID: w.ID(),
			Actor: identity.Actor{Kind: identity.ActorSystem}, After: w.Snapshot(), OccurredAt: clock.Now(),
		}
		if err := p.w.Audit.Record(ctx, e); err != nil {
			return fmt.Errorf("record %s: %w", e.Type, err)
		}
		return nil
	})
}

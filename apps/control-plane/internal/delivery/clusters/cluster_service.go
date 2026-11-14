// Package clusters implements delivery.ClusterService: the Organization-level
// Clusters that record which runtime agent serves which Kubernetes cluster.
// The domain type stays in delivery; this package orchestrates it.
//
// NewClusterService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and records
// the decision that allowed each change in its audit record.
package clusters

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

// ClusterServiceOption configures the Cluster service.
type ClusterServiceOption func(*options)

type options struct {
	clusters delivery.ClusterRepository
	revoker  delivery.AgentRevoker
	authz    authorization.Authorizer
	writer   change.Writer
}

func WithClusterRepo(r delivery.ClusterRepository) ClusterServiceOption {
	return func(o *options) { o.clusters = r }
}

// WithAgentRevoker makes archiving a Cluster suspend its agent and revoke its
// credentials in the same transaction.
func WithAgentRevoker(r delivery.AgentRevoker) ClusterServiceOption {
	return func(o *options) { o.revoker = r }
}
func WithAuthorizer(a authorization.Authorizer) ClusterServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must write
// through its context.
func WithTransactor(tx transaction.Transactor) ClusterServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ClusterServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ClusterServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewClusterService returns the ClusterService, authorizing every operation,
// or an error naming a missing dependency.
func NewClusterService(opts ...ClusterServiceOption) (delivery.ClusterService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"ClusterRepo", o.clusters != nil},
		{"AgentRevoker", o.revoker != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("cluster service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("cluster service: %w", err)
	}
	return &authorized{next: &service{clusters: o.clusters, revoker: o.revoker, w: o.writer}, authz: o.authz}, nil
}

// service carries out Cluster operations once they are authorized. It checks
// rules that span records, changes objects only through their methods, and
// commits each change with its events and audit record.
type service struct {
	clusters delivery.ClusterRepository
	revoker  delivery.AgentRevoker
	w        change.Writer
}

var _ delivery.ClusterService = (*service)(nil)

// CreateCluster creates a Cluster in the caller's Organization.
func (s *service) CreateCluster(ctx context.Context, actor identity.Actor, in delivery.CreateClusterInput) (*delivery.Cluster, error) {
	c, err := delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: in.OrganizationID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Runtime:        in.Runtime,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.clusters.CreateCluster(ctx, c); err != nil {
			return fmt.Errorf("create cluster: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateCluster, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: delivery.EventClusterCreated, OrganizationID: c.OrganizationID(),
			ResourceID: c.ID(), Actor: actor, After: c.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *service) GetCluster(ctx context.Context, _ identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	return s.clusters.GetCluster(ctx, organizationID, clusterID)
}

func (s *service) ListClusters(ctx context.Context, _ identity.Actor, query delivery.ListClustersQuery) (*pagination.Result[*delivery.Cluster], error) {
	return s.clusters.ListClusters(ctx, query)
}

// UpdateCluster changes an active Cluster's display name.
func (s *service) UpdateCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string, props delivery.UpdateClusterProps) (*delivery.Cluster, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Cluster]{
		AggregateType: delivery.AggregateCluster,
		AggregateID:   func(c *delivery.Cluster) string { return c.ID() },
		Load: func(ctx context.Context) (*delivery.Cluster, error) {
			return s.clusters.GetCluster(ctx, organizationID, clusterID)
		},
		Apply:    func(_ context.Context, c *delivery.Cluster) error { return c.Update(props) },
		Save:     s.clusters.UpdateCluster,
		Snapshot: func(c *delivery.Cluster) any { return c.Snapshot() },
		Event: func(c *delivery.Cluster) audit.Event {
			return audit.Event{Type: delivery.EventClusterUpdated, OrganizationID: organizationID, ResourceID: c.ID()}
		},
	})
}

// ArchiveCluster archives a Cluster, suspends its agent, and revokes the
// agent's credentials, all in one transaction. Archiving an archived Cluster
// does nothing.
func (s *service) ArchiveCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	var out *delivery.Cluster
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := s.clusters.GetClusterForUpdate(ctx, organizationID, clusterID)
		if err != nil {
			return err
		}
		out = c
		if c.Archived() {
			return nil
		}
		before := c.Snapshot()
		principal := c.Archive()
		if err := s.clusters.UpdateCluster(ctx, c); err != nil {
			return fmt.Errorf("archive cluster %s: %w", clusterID, err)
		}
		if principal != nil {
			if err := s.revoker.RevokeAgent(ctx, organizationID, *principal); err != nil {
				return fmt.Errorf("revoke the agent of cluster %s: %w", clusterID, err)
			}
		}
		if err := s.w.Events(ctx, delivery.AggregateCluster, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: delivery.EventClusterArchived, OrganizationID: organizationID,
			ResourceID: c.ID(), Actor: actor, Before: before, After: c.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RestoreCluster restores an archived Cluster. It has no agent until a new
// enrolment token is redeemed. Restoring an active Cluster does nothing.
func (s *service) RestoreCluster(ctx context.Context, actor identity.Actor, organizationID, clusterID string) (*delivery.Cluster, error) {
	var out *delivery.Cluster
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := s.clusters.GetClusterForUpdate(ctx, organizationID, clusterID)
		if err != nil {
			return err
		}
		out = c
		if !c.Archived() {
			return nil
		}
		before := c.Snapshot()
		c.Restore()
		if err := s.clusters.UpdateCluster(ctx, c); err != nil {
			return fmt.Errorf("restore cluster %s: %w", clusterID, err)
		}
		if err := s.w.Events(ctx, delivery.AggregateCluster, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: delivery.EventClusterRestored, OrganizationID: organizationID,
			ResourceID: c.ID(), Actor: actor, Before: before, After: c.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

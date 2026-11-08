// Package sources implements versioncontrol.SourceService: the repositories
// connected under a Project, and their HTTP API.
//
// NewSourceService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. The service below assumes
// that check has happened and records the decision that allowed each change
// in its audit record.
package sources

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type Option func(*options)

type options struct {
	sources     versioncontrol.SourceRepository
	connections versioncontrol.ConnectionRepository
	provider    vcs.Provider
	workspaces  versioncontrol.Workspaces
	resources   iam.ResourceRegistrar
	authz       authorization.Authorizer
	writer      change.Writer
}

func WithSourceRepo(r versioncontrol.SourceRepository) Option {
	return func(o *options) { o.sources = r }
}

func WithConnectionRepo(r versioncontrol.ConnectionRepository) Option {
	return func(o *options) { o.connections = r }
}

// WithProvider reads repository metadata from the provider, so a Source
// stores what the provider says rather than what a client claims. It is
// optional: without one, creating a Source fails with a clear error, and the
// control plane still starts.
func WithProvider(p vcs.Provider) Option {
	return func(o *options) { o.provider = p }
}

func WithWorkspaces(w versioncontrol.Workspaces) Option {
	return func(o *options) { o.workspaces = w }
}

// WithResourceRegistrar keeps IAM's resource hierarchy in step: a Source is
// a resource under its Project.
func WithResourceRegistrar(r iam.ResourceRegistrar) Option {
	return func(o *options) { o.resources = r }
}

func WithAuthorizer(a authorization.Authorizer) Option {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, its IAM resource, and its audit
// record commit together.
func WithTransactor(tx transaction.Transactor) Option {
	return func(o *options) { o.writer.Tx = tx }
}

func WithOutboxRecorder(r outbox.Recorder) Option {
	return func(o *options) { o.writer.Outbox = r }
}

func WithAuditRecorder(r audit.Recorder) Option {
	return func(o *options) { o.writer.Audit = r }
}

// NewSourceService returns the SourceService, authorizing every operation,
// or an error naming a missing dependency.
func NewSourceService(opts ...Option) (versioncontrol.SourceService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"SourceRepo", o.sources != nil},
		{"ConnectionRepo", o.connections != nil},
		{"Workspaces", o.workspaces != nil},
		{"ResourceRegistrar", o.resources != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("source service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("source service: %w", err)
	}
	s := &service{
		sources: o.sources, connections: o.connections, provider: o.provider,
		workspaces: o.workspaces, resources: o.resources, w: o.writer,
	}
	return &authorized{next: s, authz: o.authz, sources: o.sources}, nil
}

type service struct {
	sources     versioncontrol.SourceRepository
	connections versioncontrol.ConnectionRepository
	provider    vcs.Provider
	workspaces  versioncontrol.Workspaces
	resources   iam.ResourceRegistrar
	w           change.Writer
}

var _ versioncontrol.SourceService = (*service)(nil)

// CreateSource connects a repository under an active Project, registering
// its IAM resource under the Project.
func (s *service) CreateSource(ctx context.Context, actor identity.Actor, in versioncontrol.CreateSourceInput) (*versioncontrol.Source, error) {
	project, err := s.activeProject(ctx, in.OrganizationID, in.ProjectID)
	if err != nil {
		return nil, err
	}
	connection, err := s.usableConnection(ctx, in.OrganizationID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	repository, err := s.repository(ctx, connection, in.ExternalRepositoryID)
	if err != nil {
		return nil, err
	}
	source, err := versioncontrol.NewSource(versioncontrol.NewSourceProps{
		OrganizationID:       in.OrganizationID,
		ConnectionID:         connection.ID(),
		Provider:             connection.Provider(),
		HostURL:              connection.HostURL(),
		WorkspaceID:          project.WorkspaceID(),
		ProjectID:            project.ID(),
		Repository:           repository,
		CreatedByPrincipalID: &actor.PrincipalID,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.sources.CreateSource(ctx, source); err != nil {
			return fmt.Errorf("create source: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateSource, source.ID(), source); err != nil {
			return err
		}
		if err := s.resources.RegisterResource(ctx, iam.Resource{
			ID: source.ID(), OrganizationID: source.OrganizationID(),
			Type: iam.ResourceSource, ParentID: source.ProjectID(),
		}); err != nil {
			return fmt.Errorf("register source resource: %w", err)
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventSourceCreated, OrganizationID: source.OrganizationID(),
			ResourceID: source.ID(), Actor: actor, After: source.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return source, nil
}

// activeProject reads a Project and refuses an archived one.
func (s *service) activeProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error) {
	project, err := s.workspaces.LookupProject(ctx, organizationID, projectID)
	if err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			return nil, fmt.Errorf("%w: project %s", versioncontrol.ErrNotFound, projectID)
		}
		return nil, err
	}
	if project.Archived() {
		return nil, fmt.Errorf("%w: project %s", versioncontrol.ErrArchived, projectID)
	}
	return project, nil
}

// usableConnection reads a Connection the control plane may look up
// through: active, not archived, and not one whose lookups run in the data
// plane.
func (s *service) usableConnection(ctx context.Context, organizationID, connectionID string) (*versioncontrol.Connection, error) {
	connection, err := s.connections.GetConnection(ctx, organizationID, connectionID)
	if err != nil {
		return nil, err
	}
	if err := connection.CheckControlPlaneLookup(); err != nil {
		return nil, err
	}
	return connection, nil
}

// repository reads the repository's metadata from the provider, so the
// Source stores what the provider says rather than what a client claims.
func (s *service) repository(ctx context.Context, connection *versioncontrol.Connection, externalRepositoryID string) (vcs.Repository, error) {
	if s.provider == nil || connection.AuthKind() != versioncontrol.AuthKindAppInstallation {
		return vcs.Repository{}, versioncontrol.ErrProviderUnsupported
	}
	repository, err := s.provider.Repository(ctx, connection.Endpoint(), externalRepositoryID)
	if err != nil {
		return vcs.Repository{}, translateProviderError(err, externalRepositoryID)
	}
	return repository, nil
}

func translateProviderError(err error, externalRepositoryID string) error {
	switch {
	case errors.Is(err, vcs.ErrNotFound):
		return versioncontrol.NewErrInvalidInput(fmt.Sprintf("the provider has no repository %s", externalRepositoryID))
	case errors.Is(err, vcs.ErrUnsupportedProvider):
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnsupported, err)
	case errors.Is(err, vcs.ErrUnauthorized):
		// The provider hides repositories an installation cannot see behind
		// a 404, so a refusal means the installation itself was suspended or
		// removed.
		return fmt.Errorf("%w: the provider refused the connection: %w", versioncontrol.ErrInactiveConnection, err)
	default:
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnavailable, err)
	}
}

// RefreshSource updates a Source from the provider's current view of the
// repository, such as after a rename or a default-branch change.
func (s *service) RefreshSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	source, err := s.sources.GetSource(ctx, organizationID, sourceID)
	if err != nil {
		return nil, err
	}
	connection, err := s.usableConnection(ctx, organizationID, source.ConnectionID())
	if err != nil {
		return nil, err
	}
	repository, err := s.repository(ctx, connection, source.ExternalRepositoryID())
	if err != nil {
		return nil, err
	}
	var out *versioncontrol.Source
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		source, err := s.sources.GetSourceForUpdate(ctx, organizationID, sourceID)
		if err != nil {
			return err
		}
		before := source.Snapshot()
		if err := source.Refresh(repository); err != nil {
			return err
		}
		if !sourceChanged(before, source) {
			out = source
			return nil
		}
		if err := s.sources.UpdateSource(ctx, source); err != nil {
			return fmt.Errorf("refresh source: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateSource, source.ID(), source); err != nil {
			return err
		}
		if err := s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventSourceUpdated, OrganizationID: organizationID,
			ResourceID: source.ID(), Actor: actor, Before: before, After: source.Snapshot(),
		}); err != nil {
			return err
		}
		out = source
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sourceChanged reports whether a refresh changed the stored metadata.
func sourceChanged(before versioncontrol.SourceSnapshot, source *versioncontrol.Source) bool {
	return before.FullName != source.FullName() ||
		before.DefaultBranch != source.DefaultBranch() ||
		before.Visibility != source.Visibility() ||
		text(before.CloneURL) != text(source.CloneURL()) ||
		text(before.WebURL) != text(source.WebURL())
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *service) GetSource(ctx context.Context, _ identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	return s.sources.GetSource(ctx, organizationID, sourceID)
}

func (s *service) ListSources(ctx context.Context, _ identity.Actor, query versioncontrol.ListSourcesQuery) (*pagination.Result[*versioncontrol.Source], error) {
	return s.sources.ListSources(ctx, query)
}

func (s *service) ArchiveSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	var out *versioncontrol.Source
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		source, err := s.sources.GetSourceForUpdate(ctx, organizationID, sourceID)
		if err != nil {
			return err
		}
		if source.Archived() {
			out = source
			return nil
		}
		before := source.Snapshot()
		source.Archive()
		if err := s.sources.UpdateSource(ctx, source); err != nil {
			return fmt.Errorf("archive source: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateSource, source.ID(), source); err != nil {
			return err
		}
		if err := s.resources.ArchiveResource(ctx, organizationID, source.ID()); err != nil {
			return fmt.Errorf("archive source resource: %w", err)
		}
		if err := s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventSourceArchived, OrganizationID: organizationID,
			ResourceID: source.ID(), Actor: actor, Before: before, After: source.Snapshot(),
		}); err != nil {
			return err
		}
		out = source
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) RestoreSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*versioncontrol.Source, error) {
	var out *versioncontrol.Source
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		source, err := s.sources.GetSourceForUpdate(ctx, organizationID, sourceID)
		if err != nil {
			return err
		}
		if !source.Archived() {
			out = source
			return nil
		}
		// A restored Source must be usable again, as if it were created now.
		if _, err := s.activeProject(ctx, organizationID, source.ProjectID()); err != nil {
			return err
		}
		if _, err := s.usableConnection(ctx, organizationID, source.ConnectionID()); err != nil {
			return err
		}
		before := source.Snapshot()
		source.Restore()
		if err := s.sources.UpdateSource(ctx, source); err != nil {
			return fmt.Errorf("restore source: %w", err)
		}
		if err := s.w.Events(ctx, versioncontrol.AggregateSource, source.ID(), source); err != nil {
			return err
		}
		if err := s.resources.RestoreResource(ctx, organizationID, source.ID()); err != nil {
			return fmt.Errorf("restore source resource: %w", err)
		}
		if err := s.w.AuditRecord(ctx, audit.Event{
			Type: versioncontrol.EventSourceRestored, OrganizationID: organizationID,
			ResourceID: source.ID(), Actor: actor, Before: before, After: source.Snapshot(),
		}); err != nil {
			return err
		}
		out = source
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

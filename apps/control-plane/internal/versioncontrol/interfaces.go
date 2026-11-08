package versioncontrol

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// ConnectionRepository persists Connections. Every method is scoped to an
// Organization. It returns ErrNotFound when no row matches.
type ConnectionRepository interface {
	CreateConnection(ctx context.Context, c *Connection) error
	GetConnection(ctx context.Context, organizationID, connectionID string) (*Connection, error)
	// GetConnectionForUpdate locks the Connection until the transaction ends.
	GetConnectionForUpdate(ctx context.Context, organizationID, connectionID string) (*Connection, error)
	ListConnections(ctx context.Context, organizationID string, includeArchived bool) ([]*Connection, error)
	UpdateConnection(ctx context.Context, c *Connection) error
}

// ListSourcesQuery lists the Sources under a Project or a Workspace. It
// sorts by "created_at" (the default) or "full_name", with the ID breaking
// ties.
type ListSourcesQuery struct {
	OrganizationID  string
	ProjectID       string
	WorkspaceID     string
	IncludeArchived bool
	Page            pagination.Query
}

// SourceRepository persists Sources. Every method is scoped to an
// Organization. It returns ErrNotFound when no row matches, and
// ErrAlreadyExists for a repository another active Source connects.
type SourceRepository interface {
	CreateSource(ctx context.Context, s *Source) error
	GetSource(ctx context.Context, organizationID, sourceID string) (*Source, error)
	// GetSourceForUpdate locks the Source until the transaction ends.
	GetSourceForUpdate(ctx context.Context, organizationID, sourceID string) (*Source, error)
	ListSources(ctx context.Context, query ListSourcesQuery) (*pagination.Result[*Source], error)
	UpdateSource(ctx context.Context, s *Source) error
}

// Workspaces reads the Project a Source sits under. workspace.Lookups
// provides it.
type Workspaces interface {
	LookupProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error)
}

// CreateConnectionInput describes a new Connection.
type CreateConnectionInput struct {
	OrganizationID      string
	Provider            vcs.ProviderKind
	HostURL             string
	AuthKind            AuthKind
	ExternalAccountID   string
	ExternalAccountName *string
	LookupFrom          vcs.LookupFrom
	LookupPool          *string
}

// CreateSourceInput describes a new Source.
type CreateSourceInput struct {
	OrganizationID       string
	ProjectID            string
	ConnectionID         string
	ExternalRepositoryID string
}

// ConnectionService manages an Organization's provider Connections.
type ConnectionService interface {
	CreateConnection(ctx context.Context, actor identity.Actor, in CreateConnectionInput) (*Connection, error)
	GetConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	ListConnections(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*Connection, error)
	ArchiveConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
}

// SourceService manages the Sources under a Project.
type SourceService interface {
	CreateSource(ctx context.Context, actor identity.Actor, in CreateSourceInput) (*Source, error)
	GetSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
	ListSources(ctx context.Context, actor identity.Actor, query ListSourcesQuery) (*pagination.Result[*Source], error)
	ArchiveSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
	RestoreSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
}

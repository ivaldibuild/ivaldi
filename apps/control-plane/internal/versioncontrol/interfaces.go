package versioncontrol

import (
	"context"
	"time"

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
	ConnectedRepositories
}

// ConnectedRepositories reports which repositories active Sources already
// connect, by repository key.
type ConnectedRepositories interface {
	// ConnectedRepositoryKeys returns those of keys that an active Source in
	// the Organization connects.
	ConnectedRepositoryKeys(ctx context.Context, organizationID string, keys []string) ([]string, error)
}

// AvailableRepository is a repository a Connection's account lets Ivaldi
// read, and whether an active Source in the Organization connects it.
type AvailableRepository struct {
	Repository vcs.Repository
	Connected  bool
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

// StateSealer seals the state an install flow carries through the provider,
// so it comes back unchanged and only until it expires. IAM's session codec
// implements it.
type StateSealer interface {
	// Encode seals v, readable as name until ttl passes.
	Encode(name string, v any, ttl time.Duration) (string, error)
	// Decode opens a value Encode sealed under name into v.
	Decode(name, value string, v any) error
}

// InstallationStart is where to send a person to install Ivaldi's app.
type InstallationStart struct {
	URL       string
	ExpiresAt time.Time
}

// CompleteInstallationInput is what the provider sent back to the frontend
// after an install: the state from StartInstallation, an authorization
// code, and the installation ID.
type CompleteInstallationInput struct {
	OrganizationID string
	Provider       vcs.ProviderKind
	State          string
	Code           string
	InstallationID string
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
	// CreateConnection records a Connection that does not use an app
	// installation. App installations are connected only through
	// StartInstallation and CompleteInstallation, where the provider
	// confirms the person can access the installation.
	CreateConnection(ctx context.Context, actor identity.Actor, in CreateConnectionInput) (*Connection, error)
	GetConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	ListConnections(ctx context.Context, actor identity.Actor, organizationID string, includeArchived bool) ([]*Connection, error)
	ArchiveConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	// SuspendConnection marks a Connection unusable, such as when its
	// provider suspends the installation.
	SuspendConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	// RevokeConnection marks a Connection unusable for good.
	RevokeConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	// ReactivateConnection makes a suspended or revoked Connection usable.
	ReactivateConnection(ctx context.Context, actor identity.Actor, organizationID, connectionID string) (*Connection, error)
	// ListRepositories lists the repositories the Connection's account lets
	// Ivaldi read, by full name, as the provider reports them now. They are
	// what a Source can connect.
	ListRepositories(ctx context.Context, actor identity.Actor, organizationID, connectionID string) ([]AvailableRepository, error)
	// StartInstallation returns where to send the actor to install Ivaldi's
	// app, with state bound to the actor and the Organization.
	StartInstallation(ctx context.Context, actor identity.Actor, organizationID string, provider vcs.ProviderKind) (*InstallationStart, error)
	// CompleteInstallation creates the Connection once the provider
	// confirms the actor can access the installation.
	CompleteInstallation(ctx context.Context, actor identity.Actor, in CompleteInstallationInput) (*Connection, error)
}

// SourceService manages the Sources under a Project.
type SourceService interface {
	CreateSource(ctx context.Context, actor identity.Actor, in CreateSourceInput) (*Source, error)
	GetSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
	ListSources(ctx context.Context, actor identity.Actor, query ListSourcesQuery) (*pagination.Result[*Source], error)
	ArchiveSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
	RestoreSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
	// RefreshSource updates a Source from the provider's current view of
	// the repository.
	RefreshSource(ctx context.Context, actor identity.Actor, organizationID, sourceID string) (*Source, error)
}

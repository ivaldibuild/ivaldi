package versioncontrol

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Source is one repository connected through a Connection and placed under
// one Project. It is registered with IAM as a resource under that Project.
// Sources are never hard-deleted, because Builds, Changes, and audit records
// refer to them.
type Source struct {
	aggregate.Root
	organizationID       string
	connectionID         string
	workspaceID          string
	projectID            string
	externalRepositoryID string
	repositoryKey        string
	fullName             string
	defaultBranch        string
	visibility           Visibility
	cloneURL             *string
	webURL               *string
	createdBy            *string
	upstreamArchivedAt   *time.Time
	createdAt            time.Time
	updatedAt            time.Time
	archivedAt           *time.Time
}

// SourceSnapshot is a Source's state, for repositories, audit records, and
// events. Its JSON form is part of the Source events' contract.
type SourceSnapshot struct {
	ID                   string     `json:"id"`
	OrganizationID       string     `json:"organization_id"`
	ConnectionID         string     `json:"connection_id"`
	WorkspaceID          string     `json:"workspace_id"`
	ProjectID            string     `json:"project_id"`
	ExternalRepositoryID string     `json:"external_repository_id"`
	RepositoryKey        string     `json:"repository_key"`
	FullName             string     `json:"full_name"`
	DefaultBranch        string     `json:"default_branch"`
	Visibility           Visibility `json:"visibility" enum:"public,private,internal"`
	CloneURL             *string    `json:"clone_url"`
	WebURL               *string    `json:"web_url"`
	CreatedByPrincipalID *string    `json:"created_by_principal_id"`
	UpstreamArchivedAt   *time.Time `json:"upstream_archived_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ArchivedAt           *time.Time `json:"archived_at"`
}

// NewSourceProps describes a Source. A new one leaves the timestamps nil; a
// repository loading a stored one sets them. A stored Source gives
// RepositoryKey; a new one gives the Connection's Provider and HostURL so
// the key can be derived.
type NewSourceProps struct {
	OrganizationID       string
	ConnectionID         string
	Provider             vcs.ProviderKind
	HostURL              string
	WorkspaceID          string
	ProjectID            string
	RepositoryKey        string
	Repository           vcs.Repository
	CreatedByPrincipalID *string
	UpstreamArchivedAt   *time.Time
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
	ArchivedAt           *time.Time
}

// NewSource returns a Source, with a new ID unless sourceID gives a stored one.
func NewSource(props NewSourceProps, sourceID *string) (*Source, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateRequired("connection ID", props.ConnectionID); err != nil {
		return nil, err
	}
	if err := validateRequired("workspace ID", props.WorkspaceID); err != nil {
		return nil, err
	}
	if err := validateRequired("project ID", props.ProjectID); err != nil {
		return nil, err
	}
	repo := props.Repository
	if err := validateText("external repository ID", repo.ExternalID, maxExternalIDLength, true); err != nil {
		return nil, err
	}
	if err := validateText("repository full name", repo.FullName, maxFullNameLength, true); err != nil {
		return nil, err
	}
	if err := validateText("default branch", repo.DefaultBranch, maxDefaultBranchLength, false); err != nil {
		return nil, err
	}
	if err := Visibility(repo.Visibility).valid(); err != nil {
		return nil, err
	}
	if err := validateText("clone URL", repo.CloneURL, maxCloneURLLength, false); err != nil {
		return nil, err
	}
	if err := validateText("web URL", repo.WebURL, maxWebURLLength, false); err != nil {
		return nil, err
	}
	key := props.RepositoryKey
	if key == "" {
		if !props.Provider.Valid() {
			return nil, NewErrInvalidInput("a new source needs a valid provider")
		}
		key = repositoryKey(props.Provider, props.HostURL, repo.ExternalID)
	}
	if err := validateText("repository key", key, maxRepositoryKeyLength, true); err != nil {
		return nil, err
	}
	root, err := newRoot(Source{}.Prefix(), sourceID)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	s := &Source{
		Root:                 root,
		organizationID:       props.OrganizationID,
		connectionID:         props.ConnectionID,
		workspaceID:          props.WorkspaceID,
		projectID:            props.ProjectID,
		externalRepositoryID: repo.ExternalID,
		repositoryKey:        key,
		fullName:             repo.FullName,
		defaultBranch:        repo.DefaultBranch,
		visibility:           Visibility(repo.Visibility),
		cloneURL:             optional.Of(repo.CloneURL),
		webURL:               optional.Of(repo.WebURL),
		createdBy:            optional.OfPtr(props.CreatedByPrincipalID),
		upstreamArchivedAt:   clock.CopyTime(props.UpstreamArchivedAt),
		createdAt:            timeOr(props.CreatedAt, now),
		updatedAt:            timeOr(props.UpdatedAt, now),
		archivedAt:           clock.CopyTime(props.ArchivedAt),
	}
	if s.IsNew() {
		s.RecordEvent(SourceCreated{s.Snapshot()})
	}
	return s, nil
}

func (Source) Prefix() string                    { return "src" }
func (s *Source) OrganizationID() string         { return s.organizationID }
func (s *Source) ConnectionID() string           { return s.connectionID }
func (s *Source) WorkspaceID() string            { return s.workspaceID }
func (s *Source) ProjectID() string              { return s.projectID }
func (s *Source) ExternalRepositoryID() string   { return s.externalRepositoryID }
func (s *Source) RepositoryKey() string          { return s.repositoryKey }
func (s *Source) FullName() string               { return s.fullName }
func (s *Source) DefaultBranch() string          { return s.defaultBranch }
func (s *Source) Visibility() Visibility         { return s.visibility }
func (s *Source) CloneURL() *string              { return copyString(s.cloneURL) }
func (s *Source) WebURL() *string                { return copyString(s.webURL) }
func (s *Source) CreatedByPrincipalID() *string  { return copyString(s.createdBy) }
func (s *Source) UpstreamArchivedAt() *time.Time { return clock.CopyTime(s.upstreamArchivedAt) }
func (s *Source) CreatedAt() time.Time           { return s.createdAt }
func (s *Source) UpdatedAt() time.Time           { return s.updatedAt }
func (s *Source) ArchivedAt() *time.Time         { return clock.CopyTime(s.archivedAt) }

// Archived reports whether the Source was disconnected.
func (s *Source) Archived() bool { return s.archivedAt != nil }

func (s *Source) Snapshot() SourceSnapshot {
	return SourceSnapshot{
		ID:                   s.ID(),
		OrganizationID:       s.organizationID,
		ConnectionID:         s.connectionID,
		WorkspaceID:          s.workspaceID,
		ProjectID:            s.projectID,
		ExternalRepositoryID: s.externalRepositoryID,
		RepositoryKey:        s.repositoryKey,
		FullName:             s.fullName,
		DefaultBranch:        s.defaultBranch,
		Visibility:           s.visibility,
		CloneURL:             copyString(s.cloneURL),
		WebURL:               copyString(s.webURL),
		CreatedByPrincipalID: copyString(s.createdBy),
		UpstreamArchivedAt:   clock.CopyTime(s.upstreamArchivedAt),
		CreatedAt:            s.createdAt,
		UpdatedAt:            s.updatedAt,
		ArchivedAt:           clock.CopyTime(s.archivedAt),
	}
}

// Archive disconnects the Source. Archiving an archived one does nothing.
func (s *Source) Archive() {
	if s.Archived() {
		return
	}
	now := clock.Now()
	s.archivedAt = &now
	s.updatedAt = now
	s.RecordEvent(SourceArchived{s.Snapshot()})
}

// Restore reconnects the Source. Restoring an active one does nothing.
func (s *Source) Restore() {
	if !s.Archived() {
		return
	}
	s.archivedAt = nil
	s.updatedAt = clock.Now()
	s.RecordEvent(SourceRestored{s.Snapshot()})
}

package versioncontrol

import (
	"fmt"
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
	if err := validateRepository(repo); err != nil {
		return nil, err
	}
	key := props.RepositoryKey
	if key == "" {
		if !props.Provider.Valid() {
			return nil, NewErrInvalidInput("a new source needs a valid provider")
		}
		key = vcs.ProviderKey(props.Provider, props.HostURL, repo.ExternalID)
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

// Describes reports whether the Source already holds the repository's
// metadata, so refreshing from it would change nothing.
func (s *Source) Describes(repository vcs.Repository) bool {
	return s.externalRepositoryID == repository.ExternalID &&
		s.fullName == repository.FullName &&
		s.defaultBranch == repository.DefaultBranch &&
		string(s.visibility) == repository.Visibility &&
		deref(s.cloneURL) == repository.CloneURL &&
		deref(s.webURL) == repository.WebURL
}

// Refresh updates an active Source from the provider's current view of the
// repository, such as after a rename or a default-branch change. Refreshing
// from metadata the Source already holds does nothing.
func (s *Source) Refresh(repository vcs.Repository) error {
	if s.Archived() {
		return fmt.Errorf("%w: source %s", ErrArchived, s.ID())
	}
	if repository.ExternalID != s.externalRepositoryID {
		return NewErrInvalidInput(fmt.Sprintf("repository %s is not source %s", repository.ExternalID, s.ID()))
	}
	if err := validateRepository(repository); err != nil {
		return err
	}
	if s.Describes(repository) {
		return nil
	}
	s.fullName = repository.FullName
	s.defaultBranch = repository.DefaultBranch
	s.visibility = Visibility(repository.Visibility)
	s.cloneURL = optional.Of(repository.CloneURL)
	s.webURL = optional.Of(repository.WebURL)
	s.updatedAt = clock.Now()
	s.RecordEvent(SourceUpdated{s.Snapshot()})
	return nil
}

// ApplyProviderView updates an active Source from the provider's view of the
// repository. archived is nil to leave upstream archival unchanged, true to
// mark the repository archived at occurredAt, and false to clear it.
// occurredAt is the provider's timestamp, not Ivaldi's clock; when it is zero,
// a Source that is not archived yet is marked at the current time. It records
// one SourceUpdated event only when something changed.
func (s *Source) ApplyProviderView(repository vcs.Repository, archived *bool, occurredAt time.Time) error {
	if s.Archived() {
		return fmt.Errorf("%w: source %s", ErrArchived, s.ID())
	}
	if repository.ExternalID != s.externalRepositoryID {
		return NewErrInvalidInput(fmt.Sprintf("repository %s is not source %s", repository.ExternalID, s.ID()))
	}
	if err := validateRepository(repository); err != nil {
		return err
	}

	metadataChanged := !s.Describes(repository)
	archiveChanged := s.applyUpstreamArchive(archived, occurredAt)
	if !metadataChanged && !archiveChanged {
		return nil
	}
	if metadataChanged {
		s.fullName = repository.FullName
		s.defaultBranch = repository.DefaultBranch
		s.visibility = Visibility(repository.Visibility)
		s.cloneURL = optional.Of(repository.CloneURL)
		s.webURL = optional.Of(repository.WebURL)
	}
	s.updatedAt = clock.Now()
	s.RecordEvent(SourceUpdated{s.Snapshot()})
	return nil
}

// applyUpstreamArchive moves upstreamArchivedAt to the provider's view and
// reports whether it changed. An older provider timestamp does not move the
// stored one backwards.
func (s *Source) applyUpstreamArchive(archived *bool, occurredAt time.Time) bool {
	if archived == nil {
		return false
	}
	if !*archived {
		if s.upstreamArchivedAt == nil {
			return false
		}
		s.upstreamArchivedAt = nil
		return true
	}
	at := occurredAt.UTC().Truncate(time.Microsecond)
	if s.upstreamArchivedAt != nil {
		if at.IsZero() || !at.After(*s.upstreamArchivedAt) {
			return false
		}
	} else if at.IsZero() {
		at = clock.Now()
	}
	s.upstreamArchivedAt = clock.CopyTime(&at)
	return true
}

// validateRepository checks the repository metadata a Source stores.
func validateRepository(repository vcs.Repository) error {
	if err := validateText("external repository ID", repository.ExternalID, maxExternalIDLength, true); err != nil {
		return err
	}
	if err := validateText("repository full name", repository.FullName, maxFullNameLength, true); err != nil {
		return err
	}
	if err := validateText("default branch", repository.DefaultBranch, maxDefaultBranchLength, false); err != nil {
		return err
	}
	if err := Visibility(repository.Visibility).valid(); err != nil {
		return err
	}
	if err := validateText("clone URL", repository.CloneURL, maxCloneURLLength, false); err != nil {
		return err
	}
	return validateText("web URL", repository.WebURL, maxWebURLLength, false)
}

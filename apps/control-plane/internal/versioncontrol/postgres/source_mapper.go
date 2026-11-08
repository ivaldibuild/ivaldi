package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type sourceMapper struct{}

func newSourceMapper() *sourceMapper { return &sourceMapper{} }

func (m *sourceMapper) ToPersistence(s *versioncontrol.Source) *sourceEntity {
	snap := s.Snapshot()
	return &sourceEntity{
		ID:                   snap.ID,
		OrganizationID:       snap.OrganizationID,
		ConnectionID:         snap.ConnectionID,
		WorkspaceID:          snap.WorkspaceID,
		ProjectID:            snap.ProjectID,
		ExternalRepositoryID: snap.ExternalRepositoryID,
		RepositoryKey:        snap.RepositoryKey,
		FullName:             snap.FullName,
		DefaultBranch:        optionalString(snap.DefaultBranch),
		Visibility:           string(snap.Visibility),
		CloneURL:             snap.CloneURL,
		WebURL:               snap.WebURL,
		CreatedByPrincipalID: snap.CreatedByPrincipalID,
		UpstreamArchivedAt:   snap.UpstreamArchivedAt,
		CreatedAt:            snap.CreatedAt,
		UpdatedAt:            snap.UpdatedAt,
		ArchivedAt:           snap.ArchivedAt,
	}
}

func (m *sourceMapper) ToDomain(e *sourceEntity) (*versioncontrol.Source, error) {
	props := versioncontrol.NewSourceProps{
		OrganizationID: e.OrganizationID,
		ConnectionID:   e.ConnectionID,
		WorkspaceID:    e.WorkspaceID,
		ProjectID:      e.ProjectID,
		RepositoryKey:  e.RepositoryKey,
		Repository: vcs.Repository{
			ExternalID:    e.ExternalRepositoryID,
			FullName:      e.FullName,
			DefaultBranch: deref(e.DefaultBranch),
			Visibility:    e.Visibility,
			CloneURL:      deref(e.CloneURL),
			WebURL:        deref(e.WebURL),
		},
		CreatedByPrincipalID: e.CreatedByPrincipalID,
		UpstreamArchivedAt:   e.UpstreamArchivedAt,
		CreatedAt:            utc(e.CreatedAt),
		UpdatedAt:            utc(e.UpdatedAt),
		ArchivedAt:           utcPtr(e.ArchivedAt),
	}
	return versioncontrol.NewSource(props, &e.ID)
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

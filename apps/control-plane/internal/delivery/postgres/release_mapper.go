package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

func releaseToPersistence(rel *delivery.Release) (*releaseEntity, []releaseBuildEntity) {
	s := rel.Snapshot()
	e := &releaseEntity{
		ID:                   s.ID,
		OrganizationID:       s.OrganizationID,
		ApplicationID:        s.ApplicationID,
		ChannelID:            s.ChannelID,
		Version:              s.Version,
		Status:               string(s.Status),
		Source:               string(s.Source),
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		PublishDecisionID:    s.PublishDecisionID,
		TriggeredBy:          s.TriggeredBy,
		TriggeredByEmail:     s.TriggeredByEmail,
		PublishedAt:          s.PublishedAt,
		WithdrawnAt:          s.WithdrawnAt,
		CreatedAt:            s.CreatedAt,
		UpdatedAt:            s.UpdatedAt,
	}

	builds := make([]releaseBuildEntity, len(s.Builds))
	for i, b := range s.Builds {
		builds[i] = releaseBuildEntity{
			OrganizationID: s.OrganizationID,
			ApplicationID:  s.ApplicationID,
			ReleaseID:      s.ID,
			ArtifactID:     b.ArtifactID,
			BuildID:        b.BuildID,
			CreatedAt:      b.CreatedAt,
		}
	}

	return e, builds
}

func releaseToDomain(e *releaseEntity, builds []releaseBuildEntity) (*delivery.Release, error) {
	props := delivery.NewReleaseProps{
		OrganizationID:       e.OrganizationID,
		ApplicationID:        e.ApplicationID,
		ChannelID:            e.ChannelID,
		Version:              e.Version,
		Status:               release.Status(e.Status),
		Source:               delivery.ReleaseSource(e.Source),
		CreatedByPrincipalID: e.CreatedByPrincipalID,
		PublishDecisionID:    e.PublishDecisionID,
		TriggeredBy:          e.TriggeredBy,
		TriggeredByEmail:     e.TriggeredByEmail,

		PublishedAt: utcPtr(e.PublishedAt),
		WithdrawnAt: utcPtr(e.WithdrawnAt),
		CreatedAt:   utc(e.CreatedAt),
		UpdatedAt:   utc(e.UpdatedAt),
	}

	for _, b := range builds {
		props.Builds = append(props.Builds, delivery.NewReleaseBuildProps{ArtifactID: b.ArtifactID, BuildID: b.BuildID, CreatedAt: utc(b.CreatedAt)})
	}

	return delivery.NewRelease(props, &e.ID)
}

func buildToPersistence(b *delivery.Build) *buildEntity {
	s := b.Snapshot()
	return &buildEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, ArtifactKind: string(s.ArtifactKind), ArtifactName: s.ArtifactName,
		Digest: s.Digest, Version: s.Version, Provenance: string(s.Provenance), SourceID: s.SourceID,
		CommitSHA: s.CommitSHA, SourceRef: s.SourceRef, CIProvider: s.CIProvider, CIRunRef: s.CIRunRef,
		ReportedTags: s.ReportedTags, BuiltAt: s.BuiltAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func buildToDomain(e *buildEntity) (*delivery.Build, error) {
	return delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: e.OrganizationID,
		ArtifactKind:   delivery.ArtifactKind(e.ArtifactKind),
		ArtifactName:   e.ArtifactName,
		Digest:         e.Digest,
		Provenance:     delivery.BuildProvenance(e.Provenance),
		Details: delivery.BuildDetails{
			Version: e.Version, SourceID: e.SourceID, CommitSHA: e.CommitSHA, SourceRef: e.SourceRef,
			CIProvider: e.CIProvider, CIRunRef: e.CIRunRef, ReportedTags: e.ReportedTags, BuiltAt: utcPtr(e.BuiltAt),
		},
		CreatedAt: utc(e.CreatedAt),
		UpdatedAt: utc(e.UpdatedAt),
	}, &e.ID)
}

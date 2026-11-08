package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
)

type changeMapper struct{}

func newChangeMapper() *changeMapper { return &changeMapper{} }

func (m *changeMapper) ToPersistence(c *provenance.Change) *changeEntity {
	s := c.Snapshot()
	return &changeEntity{
		ID:                   s.ID,
		OrganizationID:       s.OrganizationID,
		SourceID:             s.SourceID,
		SessionID:            s.SessionID,
		WorkItemID:           s.WorkItemID,
		Title:                s.Title,
		ExternalRef:          s.ExternalRef,
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            s.CreatedAt,
		UpdatedAt:            s.UpdatedAt,
	}
}

func (m *changeMapper) commits(c *provenance.Change) []changeCommitEntity {
	s := c.Snapshot()
	out := make([]changeCommitEntity, 0, len(s.CommitSHAs))
	for _, sha := range s.CommitSHAs {
		out = append(out, changeCommitEntity{
			OrganizationID: s.OrganizationID, SourceID: s.SourceID, CommitSHA: sha, ChangeID: s.ID,
		})
	}
	return out
}

func (m *changeMapper) ToDomain(e *changeEntity, commits []string) (*provenance.Change, error) {
	return provenance.NewChange(provenance.NewChangeProps{
		OrganizationID:       e.OrganizationID,
		SourceID:             e.SourceID,
		SessionID:            e.SessionID,
		WorkItemID:           e.WorkItemID,
		Title:                e.Title,
		ExternalRef:          e.ExternalRef,
		CommitSHAs:           commits,
		CreatedByPrincipalID: e.CreatedByPrincipalID,
		CreatedAt:            utc(e.CreatedAt),
		UpdatedAt:            utc(e.UpdatedAt),
	}, &e.ID)
}

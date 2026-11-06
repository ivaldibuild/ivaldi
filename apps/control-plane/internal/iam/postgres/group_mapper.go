package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type groupMapper struct{}

func newGroupMapper() *groupMapper {
	return &groupMapper{}
}

func (m *groupMapper) ToPersistence(g *iam.Group) *groupEntity {
	s := g.Snapshot()
	return &groupEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		Name:           s.Name,
		Source:         string(s.Source),
		ExternalID:     s.ExternalID,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func (m *groupMapper) ToDomain(e *groupEntity) (*iam.Group, error) {
	return iam.NewGroup(iam.NewGroupProps{
		OrganizationID: e.OrganizationID,
		Name:           e.Name,
		Source:         iam.GroupSource(e.Source),
		ExternalID:     e.ExternalID,
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
	}, &e.ID)
}

func (m *groupMapper) MemberToPersistence(gm *iam.GroupMember) groupMemberEntity {
	s := gm.Snapshot()
	return groupMemberEntity{
		OrganizationID: s.OrganizationID,
		GroupID:        s.GroupID,
		PrincipalID:    s.PrincipalID,
		CreatedAt:      s.CreatedAt,
	}
}

package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

type agentMapper struct{}

func newAgentMapper() *agentMapper {
	return &agentMapper{}
}

// ToPersistence returns the Agent's principals row and its agents row.
func (m *agentMapper) ToPersistence(a *iam.Agent) (*principalEntity, *agentEntity, error) {
	s := a.Snapshot()
	p, err := principalRow(s.PrincipalSnapshot)
	if err != nil {
		return nil, nil, err
	}
	return p, &agentEntity{PrincipalID: s.ID, OrganizationID: s.OrganizationID, Slug: s.Slug}, nil
}

func (m *agentMapper) ToDomain(p *principalEntity, a *agentEntity) (*iam.Agent, error) {
	props, err := principalProps(p)
	if err != nil {
		return nil, err
	}
	return iam.NewAgent(iam.NewAgentProps{PrincipalProps: props, Slug: a.Slug}, &p.ID)
}

package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type lifecycleMapper struct{}

func newLifecycleMapper() *lifecycleMapper {
	return &lifecycleMapper{}
}

// lifecycleRows are the rows of one Lifecycle.
type lifecycleRows struct {
	lifecycle    *lifecycleEntity
	phases       []lifecyclePhaseEntity
	environments []lifecyclePhaseEnvironmentEntity
}

func (m *lifecycleMapper) ToPersistence(l *delivery.Lifecycle) lifecycleRows {
	s := l.Snapshot()
	rows := lifecycleRows{lifecycle: &lifecycleEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		WorkspaceID:    s.WorkspaceID,
		Slug:           s.Slug,
		DisplayName:    s.DisplayName,
		Description:    s.Description,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		DeletedAt:      s.DeletedAt,
	}}
	for _, p := range s.Phases {
		rows.phases = append(rows.phases, lifecyclePhaseEntity{
			ID:              p.ID,
			OrganizationID:  s.OrganizationID,
			LifecycleID:     s.ID,
			Position:        p.Position,
			Name:            p.Name,
			Optional:        p.Optional,
			MinEnvironments: p.MinEnvironments,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		})
		for _, envID := range p.EnvironmentIDs {
			rows.environments = append(rows.environments, lifecyclePhaseEnvironmentEntity{
				OrganizationID: s.OrganizationID,
				LifecycleID:    s.ID,
				PhaseID:        p.ID,
				EnvironmentID:  envID,
			})
		}
	}
	return rows
}

// ToDomain builds a Lifecycle from its row, its phases in position order,
// and their environments.
func (m *lifecycleMapper) ToDomain(rows lifecycleRows) (*delivery.Lifecycle, error) {
	envsByPhase := map[string][]string{}
	for _, e := range rows.environments {
		envsByPhase[e.PhaseID] = append(envsByPhase[e.PhaseID], e.EnvironmentID)
	}
	phases := make([]delivery.NewLifecyclePhaseProps, len(rows.phases))
	for i, p := range rows.phases {
		phases[i] = delivery.NewLifecyclePhaseProps{
			ID:              &p.ID,
			Name:            p.Name,
			Optional:        p.Optional,
			MinEnvironments: p.MinEnvironments,
			EnvironmentIDs:  envsByPhase[p.ID],
			CreatedAt:       utc(p.CreatedAt),
			UpdatedAt:       utc(p.UpdatedAt),
		}
	}
	l := rows.lifecycle
	return delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: l.OrganizationID,
		WorkspaceID:    l.WorkspaceID,
		Slug:           l.Slug,
		DisplayName:    l.DisplayName,
		Description:    l.Description,
		Phases:         phases,
		CreatedAt:      utc(l.CreatedAt),
		UpdatedAt:      utc(l.UpdatedAt),
		DeletedAt:      utcPtr(l.DeletedAt),
	}, &l.ID)
}

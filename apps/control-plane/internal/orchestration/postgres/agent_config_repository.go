package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type agentConfigRepository struct {
	db *gorm.DB
}

// NewAgentConfigRepository returns the PostgreSQL AgentConfigRepository. It
// uses the transaction carried by the context when there is one.
func NewAgentConfigRepository(db *gorm.DB) orchestration.AgentConfigRepository {
	return &agentConfigRepository{db: db}
}

func (r *agentConfigRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *agentConfigRepository) Get(ctx context.Context, organizationID, agentID string) (*orchestration.AgentConfig, error) {
	var e agentConfigEntity
	err := r.conn(ctx).Where("organization_id = ? AND agent_id = ?", organizationID, agentID).Take(&e).Error
	if err != nil {
		return nil, translate(err)
	}
	return toDomain(&e)
}

func (r *agentConfigRepository) ListByAgentIDs(ctx context.Context, organizationID string, agentIDs []string) (map[string]*orchestration.AgentConfig, error) {
	out := map[string]*orchestration.AgentConfig{}
	if len(agentIDs) == 0 {
		return out, nil
	}
	var rows []agentConfigEntity
	err := r.conn(ctx).
		Where("organization_id = ? AND agent_id IN ?", organizationID, agentIDs).
		Find(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	for i := range rows {
		config, err := toDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out[config.AgentID()] = config
	}
	return out, nil
}

func (r *agentConfigRepository) Upsert(ctx context.Context, config *orchestration.AgentConfig) error {
	s := config.Snapshot()
	e := &agentConfigEntity{
		OrganizationID: s.OrganizationID,
		AgentID:        s.AgentID,
		SystemPrompt:   s.SystemPrompt,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"system_prompt", "updated_at"}),
	}).Create(e).Error)
}

func toDomain(e *agentConfigEntity) (*orchestration.AgentConfig, error) {
	return orchestration.NewAgentConfig(orchestration.NewAgentConfigProps{
		OrganizationID: e.OrganizationID,
		AgentID:        e.AgentID,
		SystemPrompt:   e.SystemPrompt,
		CreatedAt:      utcPtr(&e.CreatedAt),
		UpdatedAt:      utcPtr(&e.UpdatedAt),
	})
}

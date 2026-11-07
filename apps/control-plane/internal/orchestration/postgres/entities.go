package postgres

import "time"

type agentConfigEntity struct {
	OrganizationID string    `gorm:"column:organization_id"`
	AgentID        string    `gorm:"column:agent_id;primaryKey"`
	SystemPrompt   string    `gorm:"column:system_prompt"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (agentConfigEntity) TableName() string { return "orchestration.agent_configs" }

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

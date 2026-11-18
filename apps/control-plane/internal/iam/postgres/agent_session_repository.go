package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type agentSessionEntity struct {
	PrincipalID          string     `gorm:"column:principal_id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	SessionID            string     `gorm:"column:session_id"`
	WorkItemID           string     `gorm:"column:work_item_id"`
	DelegatorPrincipalID string     `gorm:"column:delegator_principal_id"`
	ExpiresAt            time.Time  `gorm:"column:expires_at"`
	RevokedAt            *time.Time `gorm:"column:revoked_at"`
}

func (agentSessionEntity) TableName() string { return "iam.agent_sessions" }

type agentSessionRepository struct{ db *gorm.DB }

// NewAgentSessionRepository returns the PostgreSQL AgentSessionRepository.
func NewAgentSessionRepository(db *gorm.DB) iam.AgentSessionRepository {
	return &agentSessionRepository{db: db}
}

func (r *agentSessionRepository) Get(ctx context.Context, organizationID, principalID string) (*iam.AgentSession, error) {
	conn := database.Conn(ctx, r.db)
	var p principalEntity
	if err := conn.Where("organization_id = ? AND id = ? AND kind = ?", organizationID, principalID, string(iam.PrincipalAgentSession)).
		Take(&p).Error; err != nil {
		return nil, translate(err)
	}
	var s agentSessionEntity
	if err := conn.Where("organization_id = ? AND principal_id = ?", organizationID, principalID).Take(&s).Error; err != nil {
		return nil, translate(err)
	}
	var scope []string
	if err := conn.Table("iam.agent_session_scopes").Where("organization_id = ? AND principal_id = ?", organizationID, principalID).
		Order("resource_id").Pluck("resource_id", &scope).Error; err != nil {
		return nil, translate(err)
	}
	props, err := principalProps(&p)
	if err != nil {
		return nil, err
	}
	return iam.NewAgentSession(iam.NewAgentSessionProps{
		PrincipalProps:       props,
		SessionID:            s.SessionID,
		WorkItemID:           s.WorkItemID,
		DelegatorPrincipalID: s.DelegatorPrincipalID,
		ExpiresAt:            s.ExpiresAt.UTC(),
		RevokedAt:            utcPtr(s.RevokedAt),
		ScopeResourceIDs:     scope,
	}, &p.ID)
}

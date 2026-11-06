// Package postgres is the policy capability's PostgreSQL repository. It
// stores and rebuilds domain objects and changes nothing about them.
package postgres

import "time"

type settingLockEntity struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	OrganizationID      string    `gorm:"column:organization_id"`
	WorkspaceID         *string   `gorm:"column:workspace_id"`
	Key                 string    `gorm:"column:key"`
	LockedByPrincipalID string    `gorm:"column:locked_by_principal_id"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

func (settingLockEntity) TableName() string { return "policy.setting_locks" }

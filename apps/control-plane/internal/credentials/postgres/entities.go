package postgres

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

// secretStoreEntity is a row of credentials.secret_stores. IDs and
// timestamps are set by the domain, never by GORM.
type secretStoreEntity struct {
	ID             string               `gorm:"column:id;primaryKey"`
	OrganizationID string               `gorm:"column:organization_id"`
	WorkspaceID    string               `gorm:"column:workspace_id"`
	Slug           string               `gorm:"column:slug"`
	DisplayName    string               `gorm:"column:display_name"`
	Kind           string               `gorm:"column:kind"`
	Settings       credentials.Settings `gorm:"column:settings;type:jsonb;serializer:json"`
	Access         credentials.Access   `gorm:"column:access;type:jsonb;serializer:json"`
	EnvironmentIDs database.TextArray   `gorm:"column:environment_ids;type:text[]"`
	CreatedAt      time.Time            `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time            `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt     *time.Time           `gorm:"column:archived_at"`
}

func (secretStoreEntity) TableName() string { return "credentials.secret_stores" }

// secretStoreGrantEntity is a row of credentials.secret_store_grants.
type secretStoreGrantEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	StoreID        string     `gorm:"column:secret_store_id"`
	DeploymentID   string     `gorm:"column:deployment_id"`
	Mode           string     `gorm:"column:mode"`
	TokenID        string     `gorm:"column:token_id"`
	ExpiresAt      time.Time  `gorm:"column:expires_at"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (secretStoreGrantEntity) TableName() string { return "credentials.secret_store_grants" }

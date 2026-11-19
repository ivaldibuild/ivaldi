// Package postgres is the policy capability's PostgreSQL repository. It
// stores and rebuilds domain objects and changes nothing about them.
package postgres

import (
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type settingLockEntity struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	OrganizationID      string    `gorm:"column:organization_id"`
	WorkspaceID         *string   `gorm:"column:workspace_id"`
	Key                 string    `gorm:"column:key"`
	LockedByPrincipalID string    `gorm:"column:locked_by_principal_id"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

func (settingLockEntity) TableName() string { return "policy.setting_locks" }

type approvalRequestEntity struct {
	ID                     string     `gorm:"column:id;primaryKey"`
	OrganizationID         string     `gorm:"column:organization_id"`
	SubjectType            string     `gorm:"column:subject_type"`
	SubjectID              string     `gorm:"column:subject_id"`
	Title                  string     `gorm:"column:title"`
	State                  string     `gorm:"column:state"`
	DecisionID             string     `gorm:"column:decision_id"`
	Rule                   string     `gorm:"column:rule"`
	Reason                 string     `gorm:"column:reason"`
	Action                 string     `gorm:"column:action"`
	Permission             string     `gorm:"column:permission"`
	ResourceID             string     `gorm:"column:resource_id"`
	ProposalDigest         string     `gorm:"column:proposal_digest"`
	Summary                []byte     `gorm:"column:summary;type:jsonb"`
	ReadResourceID         string     `gorm:"column:read_resource_id"`
	OwnerTeamID            *string    `gorm:"column:owner_team_id"`
	RequestedByPrincipalID string     `gorm:"column:requested_by_principal_id"`
	SessionID              *string    `gorm:"column:session_id"`
	OnBehalfOfPrincipalID  *string    `gorm:"column:on_behalf_of_principal_id"`
	ClosedByPrincipalID    *string    `gorm:"column:closed_by_principal_id"`
	ClosedDecisionID       *string    `gorm:"column:closed_decision_id"`
	Note                   *string    `gorm:"column:note"`
	ClosedAt               *time.Time `gorm:"column:closed_at"`
	CreatedAt              time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt              time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (approvalRequestEntity) TableName() string { return "policy.approval_requests" }

type approvalRequirementEntity struct {
	OrganizationID    string             `gorm:"column:organization_id"`
	ApprovalRequestID string             `gorm:"column:approval_request_id;primaryKey"`
	Permission        string             `gorm:"column:permission;primaryKey"`
	ResourceID        string             `gorm:"column:resource_id;primaryKey"`
	ResourcePath      database.TextArray `gorm:"column:resource_path"`
}

func (approvalRequirementEntity) TableName() string { return "policy.approval_requirements" }

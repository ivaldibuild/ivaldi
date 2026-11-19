// Package postgres is the notifications capability's PostgreSQL repository.
// It stores and rebuilds notifications and changes nothing about them.
package postgres

import "time"

type notificationEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	RecipientPrincipalID string     `gorm:"column:recipient_principal_id"`
	Kind                 string     `gorm:"column:kind"`
	SubjectType          string     `gorm:"column:subject_type"`
	SubjectID            string     `gorm:"column:subject_id"`
	EventID              string     `gorm:"column:event_id"`
	Title                string     `gorm:"column:title"`
	Body                 string     `gorm:"column:body"`
	Link                 string     `gorm:"column:link"`
	OwnerTeamID          *string    `gorm:"column:owner_team_id"`
	ReadAt               *time.Time `gorm:"column:read_at"`
	ResolvedAt           *time.Time `gorm:"column:resolved_at"`
	EmailStatus          string     `gorm:"column:email_status"`
	EmailedAt            *time.Time `gorm:"column:emailed_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (notificationEntity) TableName() string { return "notifications.notifications" }

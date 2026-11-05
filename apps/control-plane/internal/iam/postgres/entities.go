package postgres

import (
	"encoding/json"
	"fmt"
	"time"
)

type organizationEntity struct {
	ID          string    `gorm:"column:id;primaryKey"`
	Slug        string    `gorm:"column:slug"`
	Name        string    `gorm:"column:name"`
	DisplayName string    `gorm:"column:display_name"`
	LogoURL     *string   `gorm:"column:logo_url"`
	Status      string    `gorm:"column:status"`
	Metadata    *string   `gorm:"column:metadata"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (organizationEntity) TableName() string { return "iam.organizations" }

type accountEntity struct {
	ID              string     `gorm:"column:id;primaryKey"`
	Email           string     `gorm:"column:email"`
	EmailVerifiedAt *time.Time `gorm:"column:email_verified_at"`
	DisplayName     string     `gorm:"column:display_name"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (accountEntity) TableName() string { return "iam.accounts" }

type passwordCredentialEntity struct {
	AccountID    string    `gorm:"column:account_id;primaryKey"`
	PasswordHash string    `gorm:"column:password_hash"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (passwordCredentialEntity) TableName() string { return "iam.password_credentials" }

// principalEntity is the row every kind of principal has; userEntity holds
// what only a User has, under the same ID.
type principalEntity struct {
	ID             string    `gorm:"column:id;primaryKey"`
	OrganizationID string    `gorm:"column:organization_id"`
	Kind           string    `gorm:"column:kind"`
	DisplayName    string    `gorm:"column:display_name"`
	Status         string    `gorm:"column:status"`
	Metadata       *string   `gorm:"column:metadata"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (principalEntity) TableName() string { return "iam.principals" }

type userEntity struct {
	PrincipalID    string    `gorm:"column:principal_id;primaryKey"`
	OrganizationID string    `gorm:"column:organization_id"`
	AccountID      string    `gorm:"column:account_id"`
	JoinedAt       time.Time `gorm:"column:joined_at"`
}

func (userEntity) TableName() string { return "iam.users" }

// utc returns t in UTC. The driver returns times in the local zone, and the
// domain records UTC.
func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return utc(*t)
}

// encodeMetadata stores no metadata as NULL rather than a JSON null.
func encodeMetadata(m map[string]string) (*string, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}
	s := string(b)
	return &s, nil
}

func decodeMetadata(s *string) (map[string]string, error) {
	if s == nil {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(*s), &m); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return m, nil
}

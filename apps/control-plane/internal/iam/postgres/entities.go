package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

// agentEntity holds what only an Agent has, under its principal's ID.
type agentEntity struct {
	PrincipalID    string `gorm:"column:principal_id;primaryKey"`
	OrganizationID string `gorm:"column:organization_id"`
	Slug           string `gorm:"column:slug"`
}

func (agentEntity) TableName() string { return "iam.agents" }

type groupEntity struct {
	ID             string    `gorm:"column:id;primaryKey"`
	OrganizationID string    `gorm:"column:organization_id"`
	Name           string    `gorm:"column:name"`
	Source         string    `gorm:"column:source"`
	ExternalID     *string   `gorm:"column:external_id"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (groupEntity) TableName() string { return "iam.groups" }

type groupMemberEntity struct {
	OrganizationID string    `gorm:"column:organization_id"`
	GroupID        string    `gorm:"column:group_id;primaryKey"`
	PrincipalID    string    `gorm:"column:principal_id;primaryKey"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

func (groupMemberEntity) TableName() string { return "iam.group_members" }

type identityProviderEntity struct {
	ID              string    `gorm:"column:id;primaryKey"`
	OrganizationID  string    `gorm:"column:organization_id"`
	Protocol        string    `gorm:"column:protocol"`
	Name            string    `gorm:"column:name"`
	Config          []byte    `gorm:"column:config;type:jsonb"`
	SecretRef       *string   `gorm:"column:secret_ref"`
	JITProvisioning bool      `gorm:"column:jit_provisioning"`
	Status          string    `gorm:"column:status"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (identityProviderEntity) TableName() string { return "iam.identity_providers" }

type externalIdentityEntity struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	OrganizationID     string     `gorm:"column:organization_id"`
	PrincipalID        string     `gorm:"column:principal_id"`
	IdentityProviderID string     `gorm:"column:identity_provider_id"`
	Subject            string     `gorm:"column:subject"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (externalIdentityEntity) TableName() string { return "iam.external_identities" }

type accountIdentityEntity struct {
	ID          string     `gorm:"column:id;primaryKey"`
	AccountID   string     `gorm:"column:account_id"`
	Provider    string     `gorm:"column:provider"`
	Subject     string     `gorm:"column:subject"`
	LastLoginAt *time.Time `gorm:"column:last_login_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (accountIdentityEntity) TableName() string { return "iam.account_identities" }

type grantEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	PrincipalID          *string    `gorm:"column:principal_id"`
	GroupID              *string    `gorm:"column:group_id"`
	RoleID               string     `gorm:"column:role_id"`
	ResourceID           string     `gorm:"column:resource_id"`
	GrantedByPrincipalID string     `gorm:"column:granted_by_principal_id"`
	ExpiresAt            *time.Time `gorm:"column:expires_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (grantEntity) TableName() string { return "iam.grants" }

type resourceEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	ResourceType   string     `gorm:"column:resource_type"`
	ParentID       *string    `gorm:"column:parent_id"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
}

func (resourceEntity) TableName() string { return "iam.resources" }

type oauthClientEntity struct {
	ID           string     `gorm:"column:id;primaryKey"`
	ClientID     string     `gorm:"column:client_id"`
	Registration string     `gorm:"column:registration"`
	Name         string     `gorm:"column:name"`
	RedirectURIs textArray  `gorm:"column:redirect_uris"`
	MetadataURL  *string    `gorm:"column:metadata_url"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	LastUsedAt   *time.Time `gorm:"column:last_used_at"`
}

func (oauthClientEntity) TableName() string { return "iam.oauth_clients" }

type authorizationCodeEntity struct {
	CodeHash       []byte     `gorm:"column:code_hash;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	PrincipalID    string     `gorm:"column:principal_id"`
	ClientID       string     `gorm:"column:client_id"`
	RedirectURI    string     `gorm:"column:redirect_uri"`
	CodeChallenge  string     `gorm:"column:code_challenge"`
	Resource       string     `gorm:"column:resource"`
	Scopes         textArray  `gorm:"column:scopes"`
	ExpiresAt      time.Time  `gorm:"column:expires_at"`
	UsedAt         *time.Time `gorm:"column:used_at"`
}

func (authorizationCodeEntity) TableName() string { return "iam.oauth_authorization_codes" }

type refreshTokenEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	PrincipalID    string     `gorm:"column:principal_id"`
	ClientID       string     `gorm:"column:client_id"`
	FamilyID       string     `gorm:"column:family_id"`
	TokenHash      []byte     `gorm:"column:token_hash"`
	Resource       string     `gorm:"column:resource"`
	Scopes         textArray  `gorm:"column:scopes"`
	ExpiresAt      time.Time  `gorm:"column:expires_at"`
	ReplacedBy     *string    `gorm:"column:replaced_by"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (refreshTokenEntity) TableName() string { return "iam.oauth_refresh_tokens" }

// textArray is a TEXT[] column. database/sql cannot scan an array, so Scan
// goes through pgx's type map; Value encodes it the same way, as PostgreSQL
// array text.
type textArray []string

// typeMaps lends pgx type maps to scans: a map is not safe for concurrent use.
var typeMaps = sync.Pool{New: func() any { return pgtype.NewMap() }}

func (a *textArray) Scan(src any) error {
	m := typeMaps.Get().(*pgtype.Map)
	defer typeMaps.Put(m)
	var v []string
	if err := m.SQLScanner(&v).Scan(src); err != nil {
		return fmt.Errorf("scan text array: %w", err)
	}
	*a = v
	return nil
}

func (a textArray) Value() (driver.Value, error) {
	m := typeMaps.Get().(*pgtype.Map)
	defer typeMaps.Put(m)
	v := []string(a)
	if v == nil {
		v = []string{}
	}
	b, err := m.Encode(pgtype.TextArrayOID, pgtype.TextFormatCode, v, nil)
	if err != nil {
		return nil, fmt.Errorf("encode text array: %w", err)
	}
	return string(b), nil
}

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

package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type workloadEntity struct {
	PrincipalID string `gorm:"column:principal_id;primaryKey"`
	// OrganizationID is nil for a platform workload.
	OrganizationID *string    `gorm:"column:organization_id"`
	Component      string     `gorm:"column:component"`
	Pool           string     `gorm:"column:pool"`
	EnrolledAt     time.Time  `gorm:"column:enrolled_at;autoCreateTime:false"`
	LastSeenAt     *time.Time `gorm:"column:last_seen_at"`
}

func (workloadEntity) TableName() string { return "iam.workloads" }

type workloadEnrollmentTokenEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	TokenHash            []byte     `gorm:"column:token_hash"`
	Pool                 string     `gorm:"column:pool"`
	ClusterID            *string    `gorm:"column:cluster_id"`
	CredentialKind       string     `gorm:"column:credential_kind"`
	CreatedByPrincipalID string     `gorm:"column:created_by_principal_id"`
	ExpiresAt            time.Time  `gorm:"column:expires_at"`
	UsedAt               *time.Time `gorm:"column:used_at"`
	UsedByPrincipalID    *string    `gorm:"column:used_by_principal_id"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (workloadEnrollmentTokenEntity) TableName() string {
	return "iam.workload_enrollment_tokens"
}

type workloadCredentialEntity struct {
	ID string `gorm:"column:id;primaryKey"`
	// OrganizationID is nil for a platform workload's credential.
	OrganizationID *string    `gorm:"column:organization_id"`
	PrincipalID    string     `gorm:"column:principal_id"`
	Kind           string     `gorm:"column:kind"`
	Identifier     string     `gorm:"column:identifier"`
	SecretHash     []byte     `gorm:"column:secret_hash"`
	PublicKey      []byte     `gorm:"column:public_key"`
	NotAfter       *time.Time `gorm:"column:not_after"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	ReplacedBy     *string    `gorm:"column:replaced_by"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
}

func (workloadCredentialEntity) TableName() string { return "iam.workload_credentials" }

type workloadRow struct {
	ID             string     `gorm:"column:id"`
	OrganizationID *string    `gorm:"column:organization_id"`
	DisplayName    string     `gorm:"column:display_name"`
	Status         string     `gorm:"column:status"`
	Metadata       *string    `gorm:"column:metadata"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	Component      string     `gorm:"column:component"`
	Pool           string     `gorm:"column:pool"`
	EnrolledAt     time.Time  `gorm:"column:enrolled_at"`
	LastSeenAt     *time.Time `gorm:"column:last_seen_at"`
}

type workloadRepository struct{ db *gorm.DB }

// NewWorkloadRepository returns the PostgreSQL WorkloadRepository.
func NewWorkloadRepository(db *gorm.DB) iam.WorkloadRepository {
	return &workloadRepository{db: db}
}

func (r *workloadRepository) conn(ctx context.Context) *gorm.DB { return database.Conn(ctx, r.db) }

func (r *workloadRepository) Write(ctx context.Context, w *iam.Workload) error {
	s := w.Snapshot()
	metadata, err := encodeMetadata(s.Metadata)
	if err != nil {
		return err
	}
	p := &principalEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, Kind: string(s.Kind), DisplayName: s.DisplayName,
		Status: string(s.Status), Metadata: metadata, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	e := &workloadEntity{
		PrincipalID: s.ID, OrganizationID: organizationColumn(s.Platform, s.OrganizationID), Component: string(s.Component),
		Pool: s.Pool, EnrolledAt: s.EnrolledAt, LastSeenAt: s.LastSeenAt,
	}
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		principals := tx
		if s.Platform {
			// A platform principal's organization_id is NULL, which the
			// shared principal entity cannot hold.
			principals = tx.Omit("organization_id")
		}
		if err := principals.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"display_name", "status", "metadata", "updated_at"}),
		}).Create(p).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "principal_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"component", "pool", "last_seen_at"}),
		}).Create(e).Error
	}))
}

func (r *workloadRepository) UpdateLastSeen(ctx context.Context, w *iam.Workload) error {
	s := w.Snapshot()
	q := r.conn(ctx).Model(&workloadEntity{})
	if s.Platform {
		q = q.Where("organization_id IS NULL AND principal_id = ?", s.ID)
	} else {
		q = q.Where("organization_id = ? AND principal_id = ?", s.OrganizationID, s.ID)
	}
	return translate(q.Update("last_seen_at", s.LastSeenAt).Error)
}

func (r *workloadRepository) Get(ctx context.Context, organizationID, principalID string) (*iam.Workload, error) {
	return r.get(r.workloads(ctx).Where("p.organization_id = ? AND p.id = ?", organizationID, principalID))
}

func (r *workloadRepository) GetPlatform(ctx context.Context, principalID string) (*iam.Workload, error) {
	return r.get(r.workloads(ctx).Where("p.organization_id IS NULL AND p.id = ?", principalID))
}

func (r *workloadRepository) ListPlatform(ctx context.Context) ([]*iam.Workload, error) {
	var rows []workloadRow
	if err := r.workloads(ctx).Where("p.organization_id IS NULL").Order("p.display_name, p.id").Find(&rows).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.Workload, len(rows))
	for i := range rows {
		w, err := workloadToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out[i] = w
	}
	return out, nil
}

// workloads selects workloads with their principals. The join is on the
// principal ID alone, so it finds platform workloads, whose organization_id
// is NULL, too.
func (r *workloadRepository) workloads(ctx context.Context) *gorm.DB {
	return r.conn(ctx).Table("iam.principals p").
		Select("p.id, p.organization_id, p.display_name, p.status, p.metadata, p.created_at, p.updated_at, w.component, w.pool, w.enrolled_at, w.last_seen_at").
		Joins("JOIN iam.workloads w ON w.principal_id = p.id")
}

func (r *workloadRepository) get(q *gorm.DB) (*iam.Workload, error) {
	var row workloadRow
	if err := q.Take(&row).Error; err != nil {
		return nil, translate(err)
	}
	return workloadToDomain(&row)
}

func workloadToDomain(row *workloadRow) (*iam.Workload, error) {
	metadata, err := decodeMetadata(row.Metadata)
	if err != nil {
		return nil, err
	}
	return iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{
			Platform: row.OrganizationID == nil, OrganizationID: deref(row.OrganizationID),
			DisplayName: row.DisplayName, Status: iam.PrincipalStatus(row.Status),
			Metadata: metadata, CreatedAt: utc(row.CreatedAt), UpdatedAt: utc(row.UpdatedAt),
		},
		Component: iam.WorkloadComponent(row.Component), Pool: row.Pool,
		EnrolledAt: utc(row.EnrolledAt), LastSeenAt: utcPtr(row.LastSeenAt),
	}, &row.ID)
}

// organizationColumn is the organization_id a workload row stores: NULL for a
// platform workload.
func organizationColumn(platform bool, organizationID string) *string {
	if platform {
		return nil
	}
	return &organizationID
}

type workloadEnrollmentTokenRepository struct{ db *gorm.DB }

// NewWorkloadEnrollmentTokenRepository returns the PostgreSQL repository.
func NewWorkloadEnrollmentTokenRepository(db *gorm.DB) iam.WorkloadEnrollmentTokenRepository {
	return &workloadEnrollmentTokenRepository{db: db}
}

func (r *workloadEnrollmentTokenRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *workloadEnrollmentTokenRepository) Write(ctx context.Context, t *iam.WorkloadEnrollmentToken) error {
	s := t.Snapshot()
	e := &workloadEnrollmentTokenEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, TokenHash: s.TokenHash, Pool: s.Pool,
		ClusterID: s.ClusterID, CredentialKind: string(s.CredentialKind),
		CreatedByPrincipalID: s.CreatedByPrincipalID, ExpiresAt: s.ExpiresAt,
		UsedAt: s.UsedAt, UsedByPrincipalID: s.UsedByPrincipalID, CreatedAt: s.CreatedAt,
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"used_at", "used_by_principal_id"}),
	}).Create(e).Error)
}

func (r *workloadEnrollmentTokenRepository) GetByHashForUpdate(ctx context.Context, hash []byte) (*iam.WorkloadEnrollmentToken, error) {
	var e workloadEnrollmentTokenEntity
	err := r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("token_hash = ?", hash).Take(&e).Error
	if err != nil {
		return nil, translate(err)
	}
	return iam.NewWorkloadEnrollmentToken(iam.NewWorkloadEnrollmentTokenProps{
		OrganizationID: e.OrganizationID, TokenHash: e.TokenHash, Pool: e.Pool,
		ClusterID: e.ClusterID, CredentialKind: iam.WorkloadCredentialKind(e.CredentialKind),
		CreatedByPrincipalID: e.CreatedByPrincipalID, ExpiresAt: *utc(e.ExpiresAt),
		UsedAt: utcPtr(e.UsedAt), UsedByPrincipalID: e.UsedByPrincipalID, CreatedAt: utc(e.CreatedAt),
	}, &e.ID)
}

type workloadCredentialRepository struct{ db *gorm.DB }

// NewWorkloadCredentialRepository returns the PostgreSQL repository.
func NewWorkloadCredentialRepository(db *gorm.DB) iam.WorkloadCredentialRepository {
	return &workloadCredentialRepository{db: db}
}

func (r *workloadCredentialRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *workloadCredentialRepository) Write(ctx context.Context, c *iam.WorkloadCredential) error {
	s := c.Snapshot()
	e := &workloadCredentialEntity{
		ID: s.ID, OrganizationID: organizationColumn(s.Platform, s.OrganizationID), PrincipalID: s.PrincipalID,
		Kind: string(s.Kind), Identifier: s.Identifier, SecretHash: s.SecretHash, PublicKey: s.PublicKey,
		NotAfter: s.NotAfter, RevokedAt: s.RevokedAt, ReplacedBy: s.ReplacedBy, CreatedAt: s.CreatedAt,
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"not_after", "revoked_at", "replaced_by"}),
	}).Create(e).Error)
}

func (r *workloadCredentialRepository) Get(ctx context.Context, organizationID, credentialID string) (*iam.WorkloadCredential, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, credentialID))
}

func (r *workloadCredentialRepository) GetBySecretHash(ctx context.Context, hash []byte) (*iam.WorkloadCredential, error) {
	return r.get(r.conn(ctx).Where("kind = ? AND secret_hash = ?", string(iam.CredentialBearerToken), hash))
}

func (r *workloadCredentialRepository) GetByKindAndIdentifier(ctx context.Context, kind iam.WorkloadCredentialKind, identifier string) (*iam.WorkloadCredential, error) {
	return r.get(r.conn(ctx).
		Where("kind = ? AND identifier = ? AND revoked_at IS NULL", string(kind), identifier))
}

func (r *workloadCredentialRepository) ListActiveByIdentity(ctx context.Context, organizationID string, kind iam.WorkloadCredentialKind, identifier string) ([]*iam.WorkloadCredential, error) {
	var entities []workloadCredentialEntity
	err := r.conn(ctx).
		Where("organization_id = ? AND kind = ? AND identifier = ? AND revoked_at IS NULL", organizationID, string(kind), identifier).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.WorkloadCredential, len(entities))
	for i := range entities {
		c, err := workloadCredentialToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func (r *workloadCredentialRepository) ListByPrincipal(ctx context.Context, organizationID, principalID string) ([]*iam.WorkloadCredential, error) {
	var entities []workloadCredentialEntity
	err := r.conn(ctx).Where("organization_id = ? AND principal_id = ?", organizationID, principalID).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.WorkloadCredential, len(entities))
	for i := range entities {
		c, err := workloadCredentialToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func (r *workloadCredentialRepository) ListPlatformByPrincipal(ctx context.Context, principalID string) ([]*iam.WorkloadCredential, error) {
	var entities []workloadCredentialEntity
	err := r.conn(ctx).Where("organization_id IS NULL AND principal_id = ?", principalID).
		Order("created_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*iam.WorkloadCredential, len(entities))
	for i := range entities {
		c, err := workloadCredentialToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func (r *workloadCredentialRepository) get(q *gorm.DB) (*iam.WorkloadCredential, error) {
	var e workloadCredentialEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return workloadCredentialToDomain(&e)
}

func workloadCredentialToDomain(e *workloadCredentialEntity) (*iam.WorkloadCredential, error) {
	return iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		Platform: e.OrganizationID == nil, OrganizationID: deref(e.OrganizationID), PrincipalID: e.PrincipalID,
		Kind: iam.WorkloadCredentialKind(e.Kind), Identifier: e.Identifier, SecretHash: e.SecretHash,
		PublicKey: e.PublicKey,
		NotAfter:  utcPtr(e.NotAfter), RevokedAt: utcPtr(e.RevokedAt), ReplacedBy: e.ReplacedBy,
		CreatedAt: utc(e.CreatedAt),
	}, &e.ID)
}

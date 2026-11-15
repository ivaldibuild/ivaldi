package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type releaseVariablesEntity struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	OrganizationID     string    `gorm:"column:organization_id"`
	ApplicationID      string    `gorm:"column:application_id"`
	ReleaseID          string    `gorm:"column:release_id"`
	Revision           int       `gorm:"column:revision"`
	Status             string    `gorm:"column:status"`
	Variables          []byte    `gorm:"column:variables;type:jsonb"`
	TakenByPrincipalID *string   `gorm:"column:taken_by_principal_id"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (releaseVariablesEntity) TableName() string { return "delivery.variable_snapshots" }

type releaseVariablesRepository struct {
	db *gorm.DB
}

// NewReleaseVariablesRepository returns the PostgreSQL
// ReleaseVariablesRepository. It uses the transaction carried by the
// context when there is one.
func NewReleaseVariablesRepository(db *gorm.DB) delivery.ReleaseVariablesRepository {
	return &releaseVariablesRepository{db: db}
}

func (r *releaseVariablesRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *releaseVariablesRepository) CreateReleaseVariables(ctx context.Context, rv *delivery.ReleaseVariables) error {
	s := rv.Snapshot()
	variables, err := json.Marshal(s.Variables)
	if err != nil {
		return fmt.Errorf("encode variables: %w", err)
	}
	return translate(r.conn(ctx).Create(&releaseVariablesEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, ReleaseID: s.ReleaseID,
		Revision: s.Revision, Status: string(s.Status), Variables: variables,
		TakenByPrincipalID: s.TakenByPrincipalID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}).Error)
}

func (r *releaseVariablesRepository) toDomain(e *releaseVariablesEntity) (*delivery.ReleaseVariables, error) {
	var variables []delivery.SnapshotVariable
	if err := json.Unmarshal(e.Variables, &variables); err != nil {
		return nil, fmt.Errorf("decode the variables of revision %s: %w", e.ID, err)
	}
	return delivery.NewReleaseVariables(delivery.NewReleaseVariablesProps{
		OrganizationID: e.OrganizationID, ApplicationID: e.ApplicationID, ReleaseID: e.ReleaseID,
		Revision: e.Revision, Status: delivery.ReleaseVariablesStatus(e.Status), Variables: variables,
		TakenByPrincipalID: e.TakenByPrincipalID, CreatedAt: utc(e.CreatedAt), UpdatedAt: utc(e.UpdatedAt),
	}, &e.ID)
}

func (r *releaseVariablesRepository) get(ctx context.Context, organizationID, where string, args ...any) (*delivery.ReleaseVariables, error) {
	var e releaseVariablesEntity
	if err := r.conn(ctx).Where("organization_id = ?", organizationID).Where(where, args...).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return r.toDomain(&e)
}

func (r *releaseVariablesRepository) GetReleaseVariables(ctx context.Context, organizationID, revisionID string) (*delivery.ReleaseVariables, error) {
	return r.get(ctx, organizationID, "id = ?", revisionID)
}

func (r *releaseVariablesRepository) GetCurrentReleaseVariables(ctx context.Context, organizationID, releaseID string) (*delivery.ReleaseVariables, error) {
	return r.get(ctx, organizationID, "release_id = ? AND status = ?", releaseID, string(delivery.ReleaseVariablesCurrent))
}

func (r *releaseVariablesRepository) LatestReleaseVariablesRevision(ctx context.Context, organizationID, releaseID string) (int, error) {
	var latest int
	err := r.conn(ctx).Model(&releaseVariablesEntity{}).
		Where("organization_id = ? AND release_id = ?", organizationID, releaseID).
		Select("COALESCE(MAX(revision), 0)").Scan(&latest).Error
	return latest, translate(err)
}

func (r *releaseVariablesRepository) UpdateReleaseVariables(ctx context.Context, rv *delivery.ReleaseVariables) error {
	s := rv.Snapshot()
	return updated(r.conn(ctx).Model(&releaseVariablesEntity{}).
		Where("organization_id = ? AND id = ?", s.OrganizationID, s.ID).
		Updates(map[string]any{"status": string(s.Status), "updated_at": s.UpdatedAt}))
}

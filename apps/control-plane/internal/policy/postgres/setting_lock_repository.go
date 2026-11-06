package postgres

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type settingLockRepository struct {
	db     *gorm.DB
	mapper settingLockMapper
}

// NewSettingLockRepository returns the PostgreSQL SettingLockRepository. It
// uses the transaction carried by the context when there is one.
func NewSettingLockRepository(db *gorm.DB) policy.SettingLockRepository {
	return &settingLockRepository{db: db}
}

func (r *settingLockRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *settingLockRepository) CreateSettingLock(ctx context.Context, l *policy.SettingLock) error {
	return translate(r.conn(ctx).Create(r.mapper.ToPersistence(l)).Error)
}

func (r *settingLockRepository) GetSettingLock(ctx context.Context, scope policy.LockScope, key string) (*policy.SettingLock, error) {
	var e settingLockEntity
	if err := atLevel(r.conn(ctx), scope).Where("key = ?", key).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&e)
}

func (r *settingLockRepository) ListSettingLocks(ctx context.Context, scope policy.LockScope) ([]*policy.SettingLock, error) {
	return r.list(atLevel(r.conn(ctx), scope))
}

func (r *settingLockRepository) ListLocksInEffect(ctx context.Context, scope policy.LockScope) ([]*policy.SettingLock, error) {
	q := r.conn(ctx).Where("organization_id = ?", scope.OrganizationID)
	if scope.WorkspaceID == "" {
		q = q.Where("workspace_id IS NULL")
	} else {
		q = q.Where("(workspace_id IS NULL OR workspace_id = ?)", scope.WorkspaceID)
	}
	return r.list(q)
}

func (r *settingLockRepository) list(q *gorm.DB) ([]*policy.SettingLock, error) {
	var rows []settingLockEntity
	if err := q.Order("key, workspace_id NULLS FIRST").Find(&rows).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]*policy.SettingLock, 0, len(rows))
	for i := range rows {
		l, err := r.mapper.ToDomain(&rows[i])
		if err != nil {
			return nil, fmt.Errorf("setting lock %s: %w", rows[i].ID, err)
		}
		out = append(out, l)
	}
	return out, nil
}

func (r *settingLockRepository) DeleteSettingLock(ctx context.Context, organizationID, lockID string) error {
	res := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, lockID).Delete(&settingLockEntity{})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return policy.ErrNotFound
	}
	return nil
}

// atLevel narrows q to the locks set at exactly the scope's level.
func atLevel(q *gorm.DB, scope policy.LockScope) *gorm.DB {
	q = q.Where("organization_id = ?", scope.OrganizationID)
	if scope.WorkspaceID == "" {
		return q.Where("workspace_id IS NULL")
	}
	return q.Where("workspace_id = ?", scope.WorkspaceID)
}

// translate maps PostgreSQL errors to policy errors.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return policy.ErrNotFound
	}
	if _, ok := database.UniqueViolation(err); ok {
		return fmt.Errorf("%w: %w", policy.ErrConflict, err)
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", policy.ErrInvalid, err)
	}
	return err
}

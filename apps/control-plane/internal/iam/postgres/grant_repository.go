package postgres

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type grantRepository struct {
	db *gorm.DB
}

// NewGrantRepository returns the PostgreSQL GrantRepository. It uses the
// transaction carried by the context when there is one.
func NewGrantRepository(db *gorm.DB) iam.GrantRepository {
	return &grantRepository{db: db}
}

func (r *grantRepository) ExistsForRole(ctx context.Context, organizationID, roleID, resourceID string) (bool, error) {
	var exists bool
	err := database.Conn(ctx, r.db).Raw(
		"SELECT EXISTS (SELECT 1 FROM iam.grants WHERE organization_id = ? AND role_id = ? AND resource_id = ?)",
		organizationID, roleID, resourceID,
	).Scan(&exists).Error
	return exists, translate(err)
}

// Write inserts the Grant. The same role on the same resource for the same
// grantee fails with ErrAlreadyExists.
func (r *grantRepository) Write(ctx context.Context, g *iam.Grant) error {
	s := g.Snapshot()
	return translate(database.Conn(ctx, r.db).Create(&grantEntity{
		ID:                   s.ID,
		OrganizationID:       s.OrganizationID,
		PrincipalID:          s.PrincipalID,
		GroupID:              s.GroupID,
		RoleID:               s.RoleID,
		ResourceID:           s.ResourceID,
		GrantedByPrincipalID: s.GrantedByPrincipalID,
		ExpiresAt:            s.ExpiresAt,
		CreatedAt:            s.CreatedAt,
	}).Error)
}

type resourceRegistrar struct {
	db *gorm.DB
}

// NewResourceRegistrar returns the PostgreSQL ResourceRegistrar. It writes
// through the transaction carried by the context, so a resource commits with
// the entity it mirrors.
func NewResourceRegistrar(db *gorm.DB) iam.ResourceRegistrar {
	return &resourceRegistrar{db: db}
}

// RegisterResource inserts the resource. A parent that does not exist fails
// with ErrNotFound, and a resource registered twice with ErrAlreadyExists.
func (r *resourceRegistrar) RegisterResource(ctx context.Context, res iam.Resource) error {
	now := clock.Now()
	e := &resourceEntity{
		ID:             res.ID,
		OrganizationID: res.OrganizationID,
		ResourceType:   string(res.Type),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if res.ParentID != "" {
		parent := res.ParentID
		e.ParentID = &parent
	}
	return translate(database.Conn(ctx, r.db).Create(e).Error)
}

func (r *resourceRegistrar) ArchiveResource(ctx context.Context, organizationID, resourceID string) error {
	now := clock.Now()
	return r.setDeletedAt(ctx, organizationID, resourceID, &now)
}

func (r *resourceRegistrar) RestoreResource(ctx context.Context, organizationID, resourceID string) error {
	return r.setDeletedAt(ctx, organizationID, resourceID, nil)
}

// setDeletedAt archives the resource, or restores it with nil. A resource
// that does not exist is ErrNotFound.
func (r *resourceRegistrar) setDeletedAt(ctx context.Context, organizationID, resourceID string, deletedAt *time.Time) error {
	res := database.Conn(ctx, r.db).Model(&resourceEntity{}).
		Where("organization_id = ? AND id = ?", organizationID, resourceID).
		Updates(map[string]any{"deleted_at": deletedAt, "updated_at": clock.Now()})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return iam.ErrNotFound
	}
	return nil
}

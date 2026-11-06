package postgres

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
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

func (r *grantRepository) Get(ctx context.Context, organizationID, grantID string) (*iam.Grant, error) {
	return r.get(database.Conn(ctx, r.db), organizationID, grantID)
}

func (r *grantRepository) GetForUpdate(ctx context.Context, organizationID, grantID string) (*iam.Grant, error) {
	return r.get(database.Conn(ctx, r.db).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}), organizationID, grantID)
}

func (r *grantRepository) get(q *gorm.DB, organizationID, grantID string) (*iam.Grant, error) {
	var e grantEntity
	if err := q.Where("organization_id = ? AND id = ?", organizationID, grantID).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return grantToDomain(&e)
}

func grantToDomain(e *grantEntity) (*iam.Grant, error) {
	return iam.NewGrant(iam.NewGrantProps{
		OrganizationID:       e.OrganizationID,
		PrincipalID:          e.PrincipalID,
		GroupID:              e.GroupID,
		RoleID:               e.RoleID,
		ResourceID:           e.ResourceID,
		GrantedByPrincipalID: e.GrantedByPrincipalID,
		ExpiresAt:            utcPtr(e.ExpiresAt),
		CreatedAt:            utc(e.CreatedAt),
	}, &e.ID)
}

func (r *grantRepository) Delete(ctx context.Context, g *iam.Grant) error {
	return translate(database.Conn(ctx, r.db).
		Where("organization_id = ? AND id = ?", g.OrganizationID(), g.ID()).
		Delete(&grantEntity{}).Error)
}

func (r *grantRepository) CountOwners(ctx context.Context, organizationID string, at time.Time) (int, error) {
	owner, _ := iam.BuiltInRoleID(iam.RoleOwner)
	var n int64
	err := database.Conn(ctx, r.db).Raw(`
		SELECT count(DISTINCT g.principal_id)
		FROM iam.grants g
		JOIN iam.principals p ON p.organization_id = g.organization_id AND p.id = g.principal_id
		WHERE g.organization_id = ? AND g.resource_id = ? AND g.role_id = ?
		  AND p.kind = ? AND p.status = ?
		  AND (g.expires_at IS NULL OR g.expires_at > ?)`,
		organizationID, organizationID, owner, string(iam.PrincipalUser), string(iam.PrincipalActive), at,
	).Scan(&n).Error
	return int(n), translate(err)
}

// grantRow is a grant with its grantee, a principal or a group, named. It
// is flat: GORM does not fill an embedded entity from a subquery's rows.
type grantRow struct {
	ID                   string     `gorm:"column:id"`
	OrganizationID       string     `gorm:"column:organization_id"`
	PrincipalID          *string    `gorm:"column:principal_id"`
	GroupID              *string    `gorm:"column:group_id"`
	RoleID               string     `gorm:"column:role_id"`
	ResourceID           string     `gorm:"column:resource_id"`
	GrantedByPrincipalID string     `gorm:"column:granted_by_principal_id"`
	ExpiresAt            *time.Time `gorm:"column:expires_at"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	PrincipalKind        *string    `gorm:"column:principal_kind"`
	PrincipalDisplayName *string    `gorm:"column:principal_display_name"`
	PrincipalStatus      *string    `gorm:"column:principal_status"`
	GroupName            *string    `gorm:"column:group_name"`
	GroupSource          *string    `gorm:"column:group_source"`
	GroupExternalID      *string    `gorm:"column:group_external_id"`
}

func (r *grantRow) entity() *grantEntity {
	return &grantEntity{
		ID: r.ID, OrganizationID: r.OrganizationID, PrincipalID: r.PrincipalID, GroupID: r.GroupID,
		RoleID: r.RoleID, ResourceID: r.ResourceID, GrantedByPrincipalID: r.GrantedByPrincipalID,
		ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt,
	}
}

var grantKeyset = database.Keyset[grantRow]{
	Keys: map[string]database.SortKey[grantRow]{
		"created_at": database.TimeKey("created_at", func(r *grantRow) time.Time { return r.CreatedAt }),
	},
	Default: "created_at",
	ID:      func(r *grantRow) string { return r.ID },
}

func (r *grantRepository) ListGrants(ctx context.Context, q iam.ListGrantsQuery) (*pagination.Result[iam.GrantEntry], error) {
	conn := database.Conn(ctx, r.db)
	base := func() *gorm.DB {
		grants := conn.Table("iam.grants g").
			Select(`g.*, p.kind AS principal_kind, p.display_name AS principal_display_name, p.status AS principal_status,
			        gr.name AS group_name, gr.source AS group_source, gr.external_id AS group_external_id`).
			Joins("LEFT JOIN iam.principals p ON p.organization_id = g.organization_id AND p.id = g.principal_id").
			Joins("LEFT JOIN iam.groups gr ON gr.organization_id = g.organization_id AND gr.id = g.group_id").
			Where("g.organization_id = ? AND g.resource_id IN ?", q.OrganizationID, q.ResourceIDs)
		if !q.IncludeExpired {
			grants = grants.Where("(g.expires_at IS NULL OR g.expires_at > ?)", q.At)
		}
		if q.GrantID != "" {
			grants = grants.Where("g.id = ?", q.GrantID)
		}
		return conn.Table("(?) AS grants", grants)
	}
	page, err := database.Paginate(base, q.Page, grantKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, iam.NewErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, translate(err)
	}
	return database.ToResult(page, func(row *grantRow) (iam.GrantEntry, error) {
		g, err := grantToDomain(row.entity())
		if err != nil {
			return iam.GrantEntry{}, err
		}
		key, _ := iam.BuiltInRoleKey(row.RoleID)
		entry := iam.GrantEntry{Grant: g.Snapshot(), RoleKey: key}
		if row.PrincipalID != nil && row.PrincipalKind != nil {
			entry.Principal = &iam.GranteePrincipal{
				ID: *row.PrincipalID, Kind: iam.PrincipalKind(*row.PrincipalKind),
				DisplayName: deref(row.PrincipalDisplayName), Status: iam.PrincipalStatus(deref(row.PrincipalStatus)),
			}
		}
		if row.GroupID != nil && row.GroupName != nil {
			entry.Group = &iam.GranteeGroup{
				ID: *row.GroupID, Name: *row.GroupName, Source: iam.GroupSource(deref(row.GroupSource)), ExternalID: row.GroupExternalID,
			}
		}
		return entry, nil
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type resourceRepository struct {
	db *gorm.DB
}

// NewResourceRepository returns the PostgreSQL ResourceRepository. It uses
// the transaction carried by the context when there is one.
func NewResourceRepository(db *gorm.DB) iam.ResourceRepository {
	return &resourceRepository{db: db}
}

func (r *resourceRepository) GetResource(ctx context.Context, organizationID, resourceID string) (iam.Resource, error) {
	var e resourceEntity
	err := database.Conn(ctx, r.db).Where("organization_id = ? AND id = ?", organizationID, resourceID).Take(&e).Error
	if err != nil {
		return iam.Resource{}, translate(err)
	}
	res := iam.Resource{ID: e.ID, OrganizationID: e.OrganizationID, Type: iam.ResourceType(e.ResourceType), Archived: e.DeletedAt != nil}
	if e.ParentID != nil {
		res.ParentID = *e.ParentID
	}
	return res, nil
}

func (r *resourceRepository) ResourceAncestors(ctx context.Context, organizationID, resourceID string) ([]string, error) {
	var ids []string
	err := database.Conn(ctx, r.db).Raw(`
		WITH RECURSIVE lineage AS (
			SELECT id, parent_id, 0 AS depth FROM iam.resources WHERE organization_id = ? AND id = ?
			UNION ALL
			SELECT r.id, r.parent_id, l.depth + 1
			FROM iam.resources r JOIN lineage l ON r.id = l.parent_id
			WHERE r.organization_id = ?
		)
		SELECT id FROM lineage ORDER BY depth`,
		organizationID, resourceID, organizationID,
	).Scan(&ids).Error
	if err != nil {
		return nil, translate(err)
	}
	if len(ids) == 0 {
		return nil, iam.ErrNotFound
	}
	return ids, nil
}

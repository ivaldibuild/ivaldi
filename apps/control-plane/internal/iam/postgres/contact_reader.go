package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type contactReader struct{ db *gorm.DB }

// NewContactReader returns the PostgreSQL ContactReader.
func NewContactReader(db *gorm.DB) iam.ContactReader {
	return &contactReader{db: db}
}

type contactRow struct {
	PrincipalID string `gorm:"column:principal_id"`
	DisplayName string `gorm:"column:display_name"`
	Email       string `gorm:"column:email"`
}

// activeUsers selects the Organization's active users with their Account
// emails; a query narrows it further.
const activeUsers = `
SELECT DISTINCT p.id AS principal_id, p.display_name, a.email
FROM iam.principals p
JOIN iam.users u ON u.organization_id = p.organization_id AND u.principal_id = p.id
JOIN iam.accounts a ON a.id = u.account_id
WHERE p.organization_id = @org AND p.kind = 'user' AND p.status = 'active'`

func (r *contactReader) ActiveUsers(ctx context.Context, organizationID string, principalIDs []string) ([]iam.Contact, error) {
	if len(principalIDs) == 0 {
		return nil, nil
	}
	return r.contacts(ctx, activeUsers+` AND p.id IN @ids ORDER BY p.id`, map[string]any{
		"org": organizationID, "ids": principalIDs,
	})
}

// administrators keeps the users granted the owner or admin role on the
// Organization's own resource, directly or through a group they are in.
const administrators = activeUsers + `
  AND EXISTS (
    SELECT 1 FROM iam.grants g
    WHERE g.organization_id = @org AND g.resource_id = @org AND g.role_id IN @roles
      AND (g.expires_at IS NULL OR g.expires_at > @at)
      AND (g.principal_id = p.id OR g.group_id IN (
        SELECT gm.group_id FROM iam.group_members gm
        WHERE gm.organization_id = @org AND gm.principal_id = p.id)))
ORDER BY p.id`

func (r *contactReader) Administrators(ctx context.Context, organizationID string) ([]iam.Contact, error) {
	owner, _ := iam.BuiltInRoleID(iam.RoleOwner)
	admin, _ := iam.BuiltInRoleID(iam.RoleAdmin)
	return r.contacts(ctx, administrators, map[string]any{
		"org": organizationID, "roles": []string{owner, admin}, "at": clock.Now(),
	})
}

func (r *contactReader) contacts(ctx context.Context, query string, args map[string]any) ([]iam.Contact, error) {
	var rows []contactRow
	if err := database.Conn(ctx, r.db).Raw(query, args).Scan(&rows).Error; err != nil {
		return nil, translate(err)
	}
	out := make([]iam.Contact, len(rows))
	for i, row := range rows {
		out[i] = iam.Contact(row)
	}
	return out, nil
}

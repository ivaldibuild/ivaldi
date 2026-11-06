package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type accessRepository struct {
	db *gorm.DB
}

// NewAccessRepository returns the PostgreSQL AccessRepository.
func NewAccessRepository(db *gorm.DB) iam.AccessRepository {
	return &accessRepository{db: db}
}

// maxResourceDepth bounds the walk up the resource hierarchy. The hierarchy
// is at most four levels deep; the bound only stops a corrupt cycle.
const maxResourceDepth = 16

// accessQuery reads the Organization's and principal's status, whether the
// resource exists, and the roles granted on it or an ancestor, in one round
// trip. Every table is filtered by the Organization.
const accessQuery = `
WITH RECURSIVE chain AS (
  SELECT r.id, r.parent_id, r.deleted_at, 0 AS depth
  FROM iam.resources r
  WHERE r.organization_id = @org AND r.id = @resource
  UNION ALL
  SELECT p.id, p.parent_id, p.deleted_at, c.depth + 1
  FROM iam.resources p
  JOIN chain c ON p.organization_id = @org AND p.id = c.parent_id
  WHERE c.depth < @depth
),
grantees AS (
  SELECT CAST(@principal AS varchar) AS principal_id, CAST(NULL AS varchar) AS group_id
  UNION ALL
  SELECT NULL, gm.group_id FROM iam.group_members gm
  WHERE gm.organization_id = @org AND gm.principal_id = @principal
)
SELECT
  EXISTS (SELECT 1 FROM iam.organizations o WHERE o.id = @org AND o.status = 'active') AS organization_active,
  (SELECT p.status FROM iam.principals p WHERE p.organization_id = @org AND p.id = @principal) AS principal_status,
  EXISTS (SELECT 1 FROM chain WHERE depth = 0) AS resource_found,
  ARRAY(
    SELECT DISTINCT g.role_id
    FROM iam.grants g
    JOIN chain c ON c.id = g.resource_id AND c.deleted_at IS NULL
    JOIN grantees e ON e.principal_id = g.principal_id OR e.group_id = g.group_id
    WHERE g.organization_id = @org AND (g.expires_at IS NULL OR g.expires_at > @at)
    ORDER BY g.role_id
  )::text[] AS role_ids
`

type accessRow struct {
	OrganizationActive bool
	PrincipalStatus    *string
	ResourceFound      bool
	RoleIDs            textArray
}

func (r *accessRepository) Access(ctx context.Context, q iam.AccessQuery) (iam.Access, error) {
	var row accessRow
	err := database.Conn(ctx, r.db).Raw(accessQuery, map[string]any{
		"org":       q.OrganizationID,
		"principal": q.PrincipalID,
		"resource":  q.ResourceID,
		"at":        q.At,
		"depth":     maxResourceDepth,
	}).Scan(&row).Error
	if err != nil {
		return iam.Access{}, translate(err)
	}
	a := iam.Access{
		OrganizationActive: row.OrganizationActive,
		ResourceFound:      row.ResourceFound,
		RoleIDs:            []string(row.RoleIDs),
	}
	if row.PrincipalStatus != nil {
		a.PrincipalStatus = iam.PrincipalStatus(*row.PrincipalStatus)
	}
	return a, nil
}

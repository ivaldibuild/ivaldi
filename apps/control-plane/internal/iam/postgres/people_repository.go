package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type peopleRepository struct {
	db     *gorm.DB
	mapper *userMapper
}

// NewPeopleRepository returns the PostgreSQL PeopleRepository. It uses the
// transaction carried by the context when there is one.
func NewPeopleRepository(db *gorm.DB) iam.PeopleRepository {
	return &peopleRepository{db: db, mapper: newUserMapper()}
}

// personRow is a user's principal, membership, and Account email.
type personRow struct {
	ID             string     `gorm:"column:id"`
	OrganizationID string     `gorm:"column:organization_id"`
	DisplayName    string     `gorm:"column:display_name"`
	Status         string     `gorm:"column:status"`
	Metadata       *string    `gorm:"column:metadata"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	AccountID      string     `gorm:"column:account_id"`
	JoinedAt       time.Time  `gorm:"column:joined_at"`
	LastSignedInAt *time.Time `gorm:"column:last_signed_in_at"`
	Email          string     `gorm:"column:email"`
}

var personKeyset = database.Keyset[personRow]{
	Keys: map[string]database.SortKey[personRow]{
		"display_name": database.StringKey("display_name", func(r *personRow) string { return r.DisplayName }),
		"created_at":   database.TimeKey("created_at", func(r *personRow) time.Time { return r.CreatedAt }),
	},
	Default: "display_name",
	ID:      func(r *personRow) string { return r.ID },
}

func (r *peopleRepository) ListPeople(ctx context.Context, q iam.ListPeopleQuery) (*pagination.Result[iam.Person], error) {
	conn := database.Conn(ctx, r.db)
	// The rows come from a subquery, so the sort columns need no table
	// qualifier and counting them is a plain COUNT.
	base := func() *gorm.DB {
		people := conn.Table("iam.principals p").
			Select(`p.id, p.organization_id, p.display_name, p.status, p.metadata, p.created_at, p.updated_at,
			        u.account_id, u.joined_at, u.last_signed_in_at, a.email`).
			Joins("JOIN iam.users u ON u.organization_id = p.organization_id AND u.principal_id = p.id").
			Joins("JOIN iam.accounts a ON a.id = u.account_id").
			Where("p.organization_id = ? AND p.kind = ?", q.OrganizationID, string(iam.PrincipalUser))
		if !q.IncludeInactive {
			people = people.Where("p.status = ?", string(iam.PrincipalActive))
		}
		if q.Search != "" {
			pattern := "%" + escapeLike(q.Search) + "%"
			people = people.Where(`(p.display_name ILIKE ? ESCAPE '\' OR a.email ILIKE ? ESCAPE '\')`, pattern, pattern)
		}
		return conn.Table("(?) AS people", people)
	}
	page, err := database.Paginate(base, q.Page, personKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, iam.NewErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, translate(err)
	}

	ids := make([]string, len(page.Rows))
	for i, row := range page.Rows {
		ids[i] = row.ID
	}
	roles, err := organizationRoles(conn, q.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	return database.ToResult(page, func(row *personRow) (iam.Person, error) {
		u, err := r.mapper.ToDomain(&principalEntity{
			ID: row.ID, OrganizationID: row.OrganizationID, Kind: string(iam.PrincipalUser),
			DisplayName: row.DisplayName, Status: row.Status, Metadata: row.Metadata,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, &userEntity{
			PrincipalID: row.ID, OrganizationID: row.OrganizationID, AccountID: row.AccountID,
			JoinedAt: row.JoinedAt, LastSignedInAt: row.LastSignedInAt,
		})
		if err != nil {
			return iam.Person{}, err
		}
		held := make([]iam.HeldRole, 0, len(roles[row.ID]))
		for _, roleID := range roles[row.ID] {
			key, _ := iam.BuiltInRoleKey(roleID)
			held = append(held, iam.HeldRole{Key: key, RoleID: roleID, ResourceID: q.OrganizationID})
		}
		return iam.Person{User: u.Snapshot(), Email: row.Email, Roles: held}, nil
	})
}

// organizationRoles returns the unexpired roles each principal holds on the
// Organization itself, granted directly or to a group they are in, sorted.
func organizationRoles(conn *gorm.DB, organizationID string, principalIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(principalIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		PrincipalID string
		RoleID      string
	}
	now := clock.Now()
	err := conn.Raw(`
		SELECT g.principal_id, g.role_id
		FROM iam.grants g
		WHERE g.organization_id = ? AND g.resource_id = ? AND g.principal_id IN ?
		  AND (g.expires_at IS NULL OR g.expires_at > ?)
		UNION
		SELECT m.principal_id, g.role_id
		FROM iam.grants g
		JOIN iam.group_members m ON m.organization_id = g.organization_id AND m.group_id = g.group_id
		WHERE g.organization_id = ? AND g.resource_id = ? AND m.principal_id IN ?
		  AND (g.expires_at IS NULL OR g.expires_at > ?)`,
		organizationID, organizationID, principalIDs, now,
		organizationID, organizationID, principalIDs, now,
	).Scan(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	for _, row := range rows {
		out[row.PrincipalID] = append(out[row.PrincipalID], row.RoleID)
	}
	for id := range out {
		slices.Sort(out[id])
	}
	return out, nil
}

// escapeLike makes s match itself in a LIKE pattern that uses '\' as its
// escape character.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

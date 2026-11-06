package postgres

import (
	"context"
	"errors"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type teamGroups struct {
	db     *gorm.DB
	mapper *groupMapper
}

// NewTeamGroups returns the PostgreSQL TeamGroups. It writes through the
// transaction carried by the context, so a team group commits with the
// change to its Team.
func NewTeamGroups(db *gorm.DB) iam.TeamGroups {
	return &teamGroups{db: db, mapper: newGroupMapper()}
}

// SyncTeamGroup finds the Team's group by its external ID, locking it, or
// creates it; renames it when name differs; and replaces its members:
// members not in memberPrincipalIDs are deleted and new ones inserted, so
// kept members keep the time they joined. It returns the group's ID.
func (r *teamGroups) SyncTeamGroup(ctx context.Context, organizationID, teamID, name string, memberPrincipalIDs []string) (string, error) {
	var groupID string
	err := translate(database.Conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		g, err := r.group(tx, organizationID, teamID, name)
		if err != nil {
			return err
		}
		groupID = g.ID()

		members := slices.Clone(memberPrincipalIDs)
		slices.Sort(members)
		members = slices.Compact(members)
		rows := make([]groupMemberEntity, len(members))
		for i, principalID := range members {
			m, err := iam.NewGroupMember(iam.NewGroupMemberProps{OrganizationID: organizationID, GroupID: g.ID(), PrincipalID: principalID})
			if err != nil {
				return err
			}
			rows[i] = r.mapper.MemberToPersistence(m)
		}

		remove := tx.Where("organization_id = ? AND group_id = ?", organizationID, g.ID())
		if len(members) > 0 {
			remove = remove.Where("principal_id NOT IN ?", members)
		}
		if err := remove.Delete(&groupMemberEntity{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "principal_id"}},
			DoNothing: true,
		}).Create(&rows).Error
	}))
	if err != nil {
		return "", err
	}
	return groupID, nil
}

// group returns the Team's group, locked until the transaction ends,
// creating it or renaming it to name as needed.
func (r *teamGroups) group(tx *gorm.DB, organizationID, teamID, name string) (*iam.Group, error) {
	var e groupEntity
	err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND source = ? AND external_id = ?", organizationID, string(iam.GroupTeam), teamID).
		Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		g, err := iam.NewGroup(iam.NewGroupProps{OrganizationID: organizationID, Name: name, Source: iam.GroupTeam, ExternalID: &teamID}, nil)
		if err != nil {
			return nil, err
		}
		return g, tx.Create(r.mapper.ToPersistence(g)).Error
	}
	if err != nil {
		return nil, err
	}
	g, err := r.mapper.ToDomain(&e)
	if err != nil {
		return nil, err
	}
	if g.Name() == name {
		return g, nil
	}
	if err := g.Rename(name); err != nil {
		return nil, err
	}
	s := g.Snapshot()
	err = tx.Model(&groupEntity{}).
		Where("organization_id = ? AND id = ?", organizationID, s.ID).
		Updates(map[string]any{"name": s.Name, "updated_at": s.UpdatedAt}).Error
	return g, err
}

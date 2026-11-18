package postgres

import (
	"context"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type deploymentLocks struct{ db *gorm.DB }

// NewDeploymentLocks returns the PostgreSQL DeploymentLocks. It locks
// through the transaction carried by the context.
func NewDeploymentLocks(db *gorm.DB) delivery.DeploymentLocks {
	return &deploymentLocks{db: db}
}

// Each statement locks one kind of row, so the order is the one
// LockForDeployment documents. Rows that do not exist are not locked; the
// deploy plan's reads report them.
const (
	lockRelease = `SELECT application_id FROM delivery.releases
WHERE organization_id = ? AND id = ? FOR SHARE`
	lockApplication = `SELECT id FROM delivery.applications
WHERE organization_id = ? AND id IN ? FOR SHARE`
	lockTargets = `SELECT id FROM delivery.targets
WHERE organization_id = ? AND id IN ? ORDER BY id FOR SHARE`
)

func (l *deploymentLocks) LockForDeployment(ctx context.Context, organizationID, releaseID string, targetIDs []string) error {
	conn := database.Conn(ctx, l.db)
	var applicationIDs []string
	if err := conn.Raw(lockRelease, organizationID, releaseID).Scan(&applicationIDs).Error; err != nil {
		return translate(err)
	}
	if len(applicationIDs) > 0 {
		if err := lock(conn, lockApplication, organizationID, applicationIDs); err != nil {
			return err
		}
	}
	if len(targetIDs) > 0 {
		return lock(conn, lockTargets, organizationID, targetIDs)
	}
	return nil
}

func lock(conn *gorm.DB, query, organizationID string, ids []string) error {
	var locked []string
	return translate(conn.Raw(query, organizationID, ids).Scan(&locked).Error)
}

package postgres

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

type deploymentEntity struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	OrganizationID       string     `gorm:"column:organization_id"`
	ApplicationID        string     `gorm:"column:application_id"`
	ReleaseID            string     `gorm:"column:release_id"`
	EnvironmentID        string     `gorm:"column:environment_id"`
	TargetID             string     `gorm:"column:target_id"`
	Source               string     `gorm:"column:source"`
	Reason               string     `gorm:"column:reason"`
	Status               string     `gorm:"column:status"`
	PreviousDeploymentID *string    `gorm:"column:previous_deployment_id"`
	InitiatedBy          *string    `gorm:"column:initiated_by_principal_id"`
	SessionID            *string    `gorm:"column:session_id"`
	WorkItemID           *string    `gorm:"column:work_item_id"`
	ExternalRef          []byte     `gorm:"column:external_ref;type:jsonb"`
	PolicyDecisionID     *string    `gorm:"column:policy_decision_id"`
	VariableSnapshotID   *string    `gorm:"column:variable_snapshot_id"`
	SecretVersions       []byte     `gorm:"column:secret_versions;type:jsonb"`
	SecretPaths          []byte     `gorm:"column:secret_paths;type:jsonb"`
	RestoresDeploymentID *string    `gorm:"column:restores_deployment_id"`
	SecretsLatest        bool       `gorm:"column:secrets_latest"`
	Findings             []byte     `gorm:"column:findings;type:jsonb"`
	RequestedAt          time.Time  `gorm:"column:requested_at;autoCreateTime:false"`
	StartedAt            *time.Time `gorm:"column:started_at"`
	FinishedAt           *time.Time `gorm:"column:finished_at"`
	SucceededAt          *time.Time `gorm:"column:succeeded_at"`
	StatusChangedAt      time.Time  `gorm:"column:status_changed_at;autoCreateTime:false"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (deploymentEntity) TableName() string { return "delivery.deployments" }

type deploymentEventEntity struct {
	ID                    string    `gorm:"column:id;primaryKey"`
	OrganizationID        string    `gorm:"column:organization_id"`
	DeploymentID          string    `gorm:"column:deployment_id"`
	Kind                  string    `gorm:"column:kind"`
	Status                *string   `gorm:"column:status"`
	Detail                []byte    `gorm:"column:detail;type:jsonb"`
	ReportID              *string   `gorm:"column:report_id"`
	ReportedByPrincipalID *string   `gorm:"column:reported_by_principal_id"`
	OccurredAt            time.Time `gorm:"column:occurred_at;autoCreateTime:false"`
	RecordedAt            time.Time `gorm:"column:recorded_at;autoUpdateTime:false"`
}

func (deploymentEventEntity) TableName() string { return "delivery.deployment_events" }

var deploymentKeyset = database.Keyset[deploymentEntity]{
	Keys: map[string]database.SortKey[deploymentEntity]{
		"requested_at": database.TimeKey("requested_at", func(e *deploymentEntity) time.Time { return e.RequestedAt }),
		"status":       database.StringKey("status", func(e *deploymentEntity) string { return e.Status }),
	},
	Default: "requested_at",
	ID:      func(e *deploymentEntity) string { return e.ID },
}

type deploymentRepository struct {
	db *gorm.DB
}

// NewDeploymentRepository returns the PostgreSQL DeploymentRepository. It
// uses the transaction carried by the context when there is one.
func NewDeploymentRepository(db *gorm.DB) delivery.DeploymentRepository {
	return &deploymentRepository{db: db}
}

func (r *deploymentRepository) conn(ctx context.Context) *gorm.DB { return database.Conn(ctx, r.db) }

func deploymentToPersistence(d *delivery.Deployment) (*deploymentEntity, error) {
	s := d.Snapshot()
	// The column must hold an array; a nil slice would marshal as null.
	if s.Findings == nil {
		s.Findings = []string{}
	}
	findings, err := json.Marshal(s.Findings)
	if err != nil {
		return nil, err
	}
	secretVersions, err := json.Marshal(s.SecretVersions)
	if err != nil {
		return nil, err
	}
	// NULL, not "null", for a Deployment whose paths were never fixed.
	var secretPaths []byte
	if s.SecretPaths != nil {
		if secretPaths, err = json.Marshal(s.SecretPaths); err != nil {
			return nil, err
		}
	}
	return &deploymentEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, ReleaseID: s.ReleaseID,
		EnvironmentID: s.EnvironmentID, TargetID: s.TargetID, Source: string(s.Source), Reason: string(s.Reason),
		Status: string(s.Status), PreviousDeploymentID: s.PreviousDeploymentID,
		InitiatedBy: s.InitiatedByPrincipalID, SessionID: s.SessionID, WorkItemID: s.WorkItemID,
		ExternalRef: s.ExternalRef, PolicyDecisionID: s.PolicyDecisionID, VariableSnapshotID: s.VariableSnapshotID, SecretVersions: secretVersions,
		SecretPaths: secretPaths, RestoresDeploymentID: s.RestoresDeploymentID, SecretsLatest: s.SecretsLatest, Findings: findings,
		RequestedAt: s.RequestedAt, StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, SucceededAt: s.SucceededAt,
		StatusChangedAt: s.StatusChangedAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}, nil
}

func deploymentToDomain(e *deploymentEntity) (*delivery.Deployment, error) {
	var findings []string
	if len(e.Findings) > 0 {
		if err := json.Unmarshal(e.Findings, &findings); err != nil {
			return nil, err
		}
	}
	var secretVersions []delivery.SecretVersion
	if len(e.SecretVersions) > 0 {
		if err := json.Unmarshal(e.SecretVersions, &secretVersions); err != nil {
			return nil, err
		}
	}
	var secretPaths map[string]string
	if len(e.SecretPaths) > 0 {
		if err := json.Unmarshal(e.SecretPaths, &secretPaths); err != nil {
			return nil, err
		}
	}
	return delivery.NewDeployment(delivery.NewDeploymentProps{
		OrganizationID: e.OrganizationID, ApplicationID: e.ApplicationID, ReleaseID: e.ReleaseID,
		EnvironmentID: e.EnvironmentID, TargetID: e.TargetID, Source: delivery.DeploymentSource(e.Source),
		Reason: delivery.DeploymentReason(e.Reason), Status: release.DeploymentStatus(e.Status),
		PreviousDeploymentID: e.PreviousDeploymentID, InitiatedByPrincipalID: e.InitiatedBy,
		SessionID: e.SessionID, WorkItemID: e.WorkItemID, ExternalRef: e.ExternalRef,
		PolicyDecisionID: e.PolicyDecisionID, VariableSnapshotID: e.VariableSnapshotID, SecretVersions: secretVersions, SecretPaths: secretPaths,
		RestoresDeploymentID: e.RestoresDeploymentID, SecretsLatest: e.SecretsLatest, Findings: findings, RequestedAt: utc(e.RequestedAt),
		StartedAt: utcPtr(e.StartedAt), FinishedAt: utcPtr(e.FinishedAt), SucceededAt: utcPtr(e.SucceededAt),
		StatusChangedAt: utc(e.StatusChangedAt), CreatedAt: utc(e.CreatedAt), UpdatedAt: utc(e.UpdatedAt),
	}, &e.ID)
}

func deploymentEventToPersistence(e *delivery.DeploymentEvent) *deploymentEventEntity {
	s := e.Snapshot()
	var status *string
	if s.Status != nil {
		v := string(*s.Status)
		status = &v
	}
	return &deploymentEventEntity{
		ID: s.ID, OrganizationID: s.OrganizationID, DeploymentID: s.DeploymentID, Kind: string(s.Kind),
		Status: status, Detail: s.Detail, ReportID: s.ReportID, ReportedByPrincipalID: s.ReportedByPrincipalID,
		OccurredAt: s.OccurredAt, RecordedAt: s.RecordedAt,
	}
}

func deploymentEventToDomain(e *deploymentEventEntity) (*delivery.DeploymentEvent, error) {
	var status *release.DeploymentStatus
	if e.Status != nil {
		s := release.DeploymentStatus(*e.Status)
		status = &s
	}
	return delivery.NewDeploymentEvent(delivery.NewDeploymentEventProps{
		OrganizationID: e.OrganizationID, DeploymentID: e.DeploymentID, Kind: delivery.DeploymentEventKind(e.Kind),
		Status: status, Detail: e.Detail, ReportID: e.ReportID, ReportedByPrincipalID: e.ReportedByPrincipalID,
		OccurredAt: utc(e.OccurredAt), RecordedAt: utc(e.RecordedAt),
	}, &e.ID)
}

func (r *deploymentRepository) CreateDeployment(ctx context.Context, d *delivery.Deployment) error {
	e, err := deploymentToPersistence(d)
	if err != nil {
		return err
	}
	return translate(r.conn(ctx).Create(e).Error)
}

func (r *deploymentRepository) UpdateDeployment(ctx context.Context, d *delivery.Deployment) error {
	e, err := deploymentToPersistence(d)
	if err != nil {
		return err
	}
	return updated(r.conn(ctx).Model(&deploymentEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"status":                 e.Status,
			"previous_deployment_id": e.PreviousDeploymentID,
			"policy_decision_id":     e.PolicyDecisionID,
			"findings":               e.Findings,
			"secret_versions":        e.SecretVersions,
			"started_at":             e.StartedAt,
			"finished_at":            e.FinishedAt,
			"succeeded_at":           e.SucceededAt,
			"status_changed_at":      e.StatusChangedAt,
			"updated_at":             e.UpdatedAt,
		}))
}

func (r *deploymentRepository) GetDeployment(ctx context.Context, organizationID, deploymentID string) (*delivery.Deployment, error) {
	return r.get(r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, deploymentID))
}

func (r *deploymentRepository) GetDeploymentForUpdate(ctx context.Context, organizationID, deploymentID string) (*delivery.Deployment, error) {
	return r.get(r.conn(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("organization_id = ? AND id = ?", organizationID, deploymentID))
}

func (r *deploymentRepository) get(q *gorm.DB) (*delivery.Deployment, error) {
	var e deploymentEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return deploymentToDomain(&e)
}

func (r *deploymentRepository) GetInFlightDeployment(ctx context.Context, organizationID, applicationID, targetID string) (*delivery.Deployment, error) {
	return r.get(r.conn(ctx).Where(
		"organization_id = ? AND application_id = ? AND target_id = ? AND finished_at IS NULL",
		organizationID, applicationID, targetID))
}

func (r *deploymentRepository) ListDeployments(ctx context.Context, query delivery.ListDeploymentsQuery) (*pagination.Result[*delivery.Deployment], error) {
	base := func() *gorm.DB {
		q := r.conn(ctx).Model(&deploymentEntity{}).Where("organization_id = ?", query.OrganizationID)
		if query.ApplicationID != "" {
			q = q.Where("application_id = ?", query.ApplicationID)
		}
		if query.ReleaseID != "" {
			q = q.Where("release_id = ?", query.ReleaseID)
		}
		if query.TargetID != "" {
			q = q.Where("target_id = ?", query.TargetID)
		}
		if query.ClusterID != "" {
			q = q.Where("target_id IN (SELECT id FROM delivery.targets WHERE organization_id = ? AND cluster_id = ? AND deleted_at IS NULL)",
				query.OrganizationID, query.ClusterID)
		}
		if query.Status != "" {
			q = q.Where("status = ?", string(query.Status))
		}
		if len(query.Statuses) > 0 {
			statuses := make([]string, len(query.Statuses))
			for i, status := range query.Statuses {
				statuses[i] = string(status)
			}
			q = q.Where("status IN ?", statuses)
		}
		return q
	}
	p, err := paginate(base, query.Page, deploymentKeyset)
	if err != nil {
		return nil, err
	}
	return database.ToResult(p, func(e *deploymentEntity) (*delivery.Deployment, error) {
		return deploymentToDomain(e)
	})
}

func (r *deploymentRepository) ListSucceededEnvironmentIDs(ctx context.Context, organizationID, releaseID string) ([]string, error) {
	var ids []string
	err := r.conn(ctx).Model(&deploymentEntity{}).
		Where("organization_id = ? AND release_id = ? AND succeeded_at IS NOT NULL", organizationID, releaseID).
		Distinct().Pluck("environment_id", &ids).Error
	return ids, translate(err)
}

func (r *deploymentRepository) GetLastSucceededDeployment(ctx context.Context, organizationID, releaseID, targetID string) (*delivery.Deployment, error) {
	return r.get(r.conn(ctx).
		Where("organization_id = ? AND release_id = ? AND target_id = ? AND succeeded_at IS NOT NULL", organizationID, releaseID, targetID).
		Order("succeeded_at DESC, id DESC"))
}

func (r *deploymentRepository) AppendDeploymentEvent(ctx context.Context, event *delivery.DeploymentEvent) (bool, error) {
	e := deploymentEventToPersistence(event)
	var detail any
	if len(e.Detail) > 0 {
		detail = string(e.Detail)
	}
	res := r.conn(ctx).Exec(`
		INSERT INTO delivery.deployment_events
			(id, organization_id, deployment_id, kind, status, detail, report_id, reported_by_principal_id, occurred_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?)
		ON CONFLICT (deployment_id, report_id) WHERE report_id IS NOT NULL DO NOTHING`,
		e.ID, e.OrganizationID, e.DeploymentID, e.Kind, e.Status, detail, e.ReportID, e.ReportedByPrincipalID,
		e.OccurredAt, e.RecordedAt)
	if res.Error != nil {
		return false, translate(res.Error)
	}
	return res.RowsAffected == 1, nil
}

func (r *deploymentRepository) ListDeploymentEvents(ctx context.Context, organizationID, deploymentID string) ([]*delivery.DeploymentEvent, error) {
	var entities []deploymentEventEntity
	err := r.conn(ctx).Where("organization_id = ? AND deployment_id = ?", organizationID, deploymentID).
		Order("occurred_at, recorded_at, id").Find(&entities).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*delivery.DeploymentEvent, len(entities))
	for i := range entities {
		e, err := deploymentEventToDomain(&entities[i])
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

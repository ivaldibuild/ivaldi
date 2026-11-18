package postgres

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type approvalRequestRepository struct {
	db     *gorm.DB
	mapper approvalRequestMapper
}

// NewApprovalRequestRepository returns the PostgreSQL
// ApprovalRequestRepository. It uses the transaction carried by the
// context when there is one.
func NewApprovalRequestRepository(db *gorm.DB) policy.ApprovalRequestRepository {
	return &approvalRequestRepository{db: db}
}

// approvalRequestKeyset sorts requests by when they were opened.
var approvalRequestKeyset = database.Keyset[approvalRequestEntity]{
	Keys: map[string]database.SortKey[approvalRequestEntity]{
		"created_at": database.TimeKey("created_at", func(e *approvalRequestEntity) time.Time { return e.CreatedAt }),
	},
	Default: "created_at",
	ID:      func(e *approvalRequestEntity) string { return e.ID },
}

func (r *approvalRequestRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *approvalRequestRepository) CreateApprovalRequest(ctx context.Context, req *policy.ApprovalRequest) error {
	e, requirements := r.mapper.ToPersistence(req)
	return translate(r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Create(&requirements).Error
	}))
}

func (r *approvalRequestRepository) UpdateApprovalRequest(ctx context.Context, req *policy.ApprovalRequest) error {
	e, _ := r.mapper.ToPersistence(req)
	res := r.conn(ctx).Model(&approvalRequestEntity{}).
		Where("organization_id = ? AND id = ?", e.OrganizationID, e.ID).
		Updates(map[string]any{
			"state": e.State, "closed_by_principal_id": e.ClosedByPrincipalID, "closed_decision_id": e.ClosedDecisionID,
			"note": e.Note, "closed_at": e.ClosedAt, "updated_at": e.UpdatedAt,
		})
	if res.Error != nil {
		return translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return policy.ErrNotFound
	}
	return nil
}

func (r *approvalRequestRepository) GetApprovalRequest(ctx context.Context, organizationID, requestID string) (*policy.ApprovalRequest, error) {
	return r.get(ctx, r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, requestID))
}

func (r *approvalRequestRepository) GetApprovalRequestForUpdate(ctx context.Context, organizationID, requestID string) (*policy.ApprovalRequest, error) {
	return r.get(ctx, r.conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("organization_id = ? AND id = ?", organizationID, requestID))
}

func (r *approvalRequestRepository) GetPendingApprovalRequestForUpdate(ctx context.Context, organizationID string, subject policy.Subject) (*policy.ApprovalRequest, error) {
	return r.get(ctx, r.conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("organization_id = ? AND subject_type = ? AND subject_id = ? AND state = ?",
			organizationID, subject.Type, subject.ID, string(policy.ApprovalPending)))
}

func (r *approvalRequestRepository) get(ctx context.Context, q *gorm.DB) (*policy.ApprovalRequest, error) {
	var e approvalRequestEntity
	if err := q.Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	requirements, err := r.requirements(ctx, e.OrganizationID, []string{e.ID})
	if err != nil {
		return nil, err
	}
	return r.mapper.ToDomain(&e, requirements[e.ID])
}

// requirements reads the requests' requirements, by request ID.
func (r *approvalRequestRepository) requirements(ctx context.Context, organizationID string, requestIDs []string) (map[string][]approvalRequirementEntity, error) {
	var rows []approvalRequirementEntity
	err := r.conn(ctx).Where("organization_id = ? AND approval_request_id IN ?", organizationID, requestIDs).
		Order("approval_request_id, permission, resource_id").Find(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make(map[string][]approvalRequirementEntity, len(requestIDs))
	for _, row := range rows {
		out[row.ApprovalRequestID] = append(out[row.ApprovalRequestID], row)
	}
	return out, nil
}

func (r *approvalRequestRepository) ListRequiredPermissions(ctx context.Context, organizationID string) ([]string, error) {
	var permissions []string
	err := r.conn(ctx).Model(&approvalRequirementEntity{}).Where("organization_id = ?", organizationID).
		Distinct().Order("permission").Pluck("permission", &permissions).Error
	return permissions, translate(err)
}

// everyRequirementHeld keeps the requests none of whose requirements falls
// outside what is held: each requirement's resource path must include a
// resource the decider holds its permission on. The held pairs are given
// as two arrays of equal length, permissions and resources.
const everyRequirementHeld = `NOT EXISTS (
  SELECT 1 FROM policy.approval_requirements q
  WHERE q.organization_id = policy.approval_requests.organization_id
    AND q.approval_request_id = policy.approval_requests.id
    AND NOT EXISTS (
      SELECT 1 FROM unnest(CAST(? AS text[]), CAST(? AS text[])) AS held(permission, resource_id)
      WHERE held.permission = q.permission AND held.resource_id = ANY(q.resource_path)))`

func (r *approvalRequestRepository) ListDecidableApprovalRequests(ctx context.Context, q policy.ListDecidableQuery) (*pagination.Result[*policy.ApprovalRequest], error) {
	var permissions, resources database.TextArray
	for permission, ids := range q.Held {
		for _, id := range ids {
			permissions = append(permissions, permission)
			resources = append(resources, id)
		}
	}
	base := func() *gorm.DB {
		b := r.conn(ctx).Model(&approvalRequestEntity{}).
			Where("organization_id = ? AND state = ?", q.OrganizationID, string(q.State)).
			Where("requested_by_principal_id <> ?", q.DeciderID).
			Where("on_behalf_of_principal_id IS DISTINCT FROM ?", q.DeciderID).
			Where(everyRequirementHeld, permissions, resources)
		if q.SubjectType != "" {
			b = b.Where("subject_type = ?", q.SubjectType)
		}
		if q.OwnerTeamID != "" {
			b = b.Where("owner_team_id = ?", q.OwnerTeamID)
		}
		return b
	}
	page, err := database.Paginate(base, q.Page, approvalRequestKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, policy.NewErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, translate(err)
	}
	ids := make([]string, len(page.Rows))
	for i, row := range page.Rows {
		ids[i] = row.ID
	}
	requirements, err := r.requirements(ctx, q.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	return database.ToResult(page, func(e *approvalRequestEntity) (*policy.ApprovalRequest, error) {
		return r.mapper.ToDomain(e, requirements[e.ID])
	})
}

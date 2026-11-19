package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

type notificationRepository struct {
	db     *gorm.DB
	mapper notificationMapper
}

// NewNotificationRepository returns the PostgreSQL NotificationRepository.
// It uses the transaction carried by the context when there is one.
func NewNotificationRepository(db *gorm.DB) notifications.NotificationRepository {
	return &notificationRepository{db: db}
}

// notificationKeyset sorts a recipient's notifications by when they were made.
var notificationKeyset = database.Keyset[notificationEntity]{
	Keys: map[string]database.SortKey[notificationEntity]{
		"created_at": database.TimeKey("created_at", func(e *notificationEntity) time.Time { return e.CreatedAt }),
	},
	Default: "created_at",
	ID:      func(e *notificationEntity) string { return e.ID },
}

func (r *notificationRepository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

func (r *notificationRepository) CreateNotifications(ctx context.Context, ns []*notifications.Notification) error {
	if len(ns) == 0 {
		return nil
	}
	rows := make([]*notificationEntity, len(ns))
	for i, n := range ns {
		rows[i] = r.mapper.ToPersistence(n)
	}
	return translate(r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "organization_id"}, {Name: "event_id"}, {Name: "recipient_principal_id"}},
		DoNothing: true,
	}).Create(&rows).Error)
}

func (r *notificationRepository) GetNotification(ctx context.Context, organizationID, notificationID string) (*notifications.Notification, error) {
	var e notificationEntity
	if err := r.conn(ctx).Where("organization_id = ? AND id = ?", organizationID, notificationID).Take(&e).Error; err != nil {
		return nil, translate(err)
	}
	return r.mapper.ToDomain(&e)
}

func (r *notificationRepository) SaveRead(ctx context.Context, n *notifications.Notification) error {
	e := r.mapper.ToPersistence(n)
	return translate(r.conn(ctx).Model(&notificationEntity{}).
		Where("organization_id = ? AND id = ? AND read_at IS NULL", e.OrganizationID, e.ID).
		Update("read_at", e.ReadAt).Error)
}

func (r *notificationRepository) SaveEmail(ctx context.Context, n *notifications.Notification) error {
	e := r.mapper.ToPersistence(n)
	return translate(r.conn(ctx).Model(&notificationEntity{}).
		Where("organization_id = ? AND id = ? AND email_status = ?", e.OrganizationID, e.ID, string(notifications.EmailPending)).
		Updates(map[string]any{"email_status": e.EmailStatus, "emailed_at": e.EmailedAt}).Error)
}

func (r *notificationRepository) ListPendingEmails(ctx context.Context, organizationID, eventID string) ([]*notifications.Notification, error) {
	var rows []notificationEntity
	err := r.conn(ctx).Where("organization_id = ? AND event_id = ? AND email_status = ?",
		organizationID, eventID, string(notifications.EmailPending)).Order("id").Find(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	out := make([]*notifications.Notification, len(rows))
	for i := range rows {
		if out[i], err = r.mapper.ToDomain(&rows[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *notificationRepository) ListNotifications(ctx context.Context, q notifications.ListNotificationsQuery) (*pagination.Result[*notifications.Notification], error) {
	base := func() *gorm.DB {
		b := r.conn(ctx).Model(&notificationEntity{}).
			Where("organization_id = ? AND recipient_principal_id = ?", q.OrganizationID, q.RecipientPrincipalID)
		if q.Unread {
			b = b.Where("read_at IS NULL")
		}
		return b
	}
	page, err := database.Paginate(base, q.Page, notificationKeyset)
	if errors.Is(err, pagination.ErrInvalidQuery) {
		return nil, notifications.NewErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, translate(err)
	}
	return database.ToResult(page, r.mapper.ToDomain)
}

func (r *notificationRepository) CountUnread(ctx context.Context, organizationID, recipientPrincipalID string) (int, error) {
	var n int64
	err := r.conn(ctx).Model(&notificationEntity{}).
		Where("organization_id = ? AND recipient_principal_id = ? AND read_at IS NULL AND resolved_at IS NULL",
			organizationID, recipientPrincipalID).
		Count(&n).Error
	return int(n), translate(err)
}

func (r *notificationRepository) MarkAllRead(ctx context.Context, organizationID, recipientPrincipalID string, at time.Time) error {
	return translate(r.conn(ctx).Model(&notificationEntity{}).
		Where("organization_id = ? AND recipient_principal_id = ? AND read_at IS NULL", organizationID, recipientPrincipalID).
		Update("read_at", at).Error)
}

func (r *notificationRepository) ResolveForSubject(ctx context.Context, organizationID string, kind notifications.Kind, subjectType, subjectID string, at time.Time) error {
	return translate(r.conn(ctx).Model(&notificationEntity{}).
		Where("organization_id = ? AND kind = ? AND subject_type = ? AND subject_id = ? AND resolved_at IS NULL",
			organizationID, string(kind), subjectType, subjectID).
		Update("resolved_at", at).Error)
}

// translate maps PostgreSQL errors to the capability's.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notifications.ErrNotFound
	}
	if _, ok := database.CheckViolation(err); ok {
		return fmt.Errorf("%w: %w", notifications.ErrInvalid, err)
	}
	return err
}

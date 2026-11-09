package postgres

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type webhookDeliveryRepository struct {
	db *gorm.DB
}

// NewWebhookDeliveryRepository returns the PostgreSQL
// WebhookDeliveryRepository. It uses the transaction carried by the context
// when there is one.
func NewWebhookDeliveryRepository(db *gorm.DB) versioncontrol.WebhookDeliveryRepository {
	return &webhookDeliveryRepository{db: db}
}

func (r *webhookDeliveryRepository) RecordWebhookDelivery(ctx context.Context, d versioncontrol.WebhookDelivery) (bool, error) {
	res := database.Conn(ctx, r.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&webhookDeliveryEntity{
		Provider:       string(d.Provider),
		RouteKey:       d.RouteKey,
		DeliveryID:     d.DeliveryID,
		OrganizationID: d.OrganizationID,
		ReceivedAt:     d.ReceivedAt.UTC(),
	})
	if res.Error != nil {
		return false, translate(res.Error)
	}
	return res.RowsAffected == 1, nil
}

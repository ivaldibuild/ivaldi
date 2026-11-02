// Package postgres implements the outbox Store, including its Recorder, with
// GORM on the outbox schema created by ../migrations.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// repository is the PostgreSQL outbox. It uses the transaction carried by the
// context when there is one (see database.Transactor), so a service's event
// commits with the change that raised it.
type repository struct {
	db *gorm.DB
}

var _ outbox.Store = (*repository)(nil)

// NewRepository returns the outbox Store on db. The concrete type stays
// private so callers depend on the interface.
func NewRepository(db *gorm.DB) outbox.Store {
	return &repository{db: db}
}

func (r *repository) conn(ctx context.Context) *gorm.DB {
	return database.Conn(ctx, r.db)
}

// Record stores envelopes, ignoring one already stored with the same ID.
func (r *repository) Record(ctx context.Context, envelopes ...events.Envelope) error {
	if len(envelopes) == 0 {
		return nil
	}
	now := clock.Now()
	rows := make([]eventEntity, len(envelopes))
	for i, e := range envelopes {
		if e.ID == "" || e.Type == "" {
			return fmt.Errorf("outbox: event %q has no ID or type", e.ID)
		}
		rows[i] = toEntity(e, now)
	}
	if err := r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("record %d event(s): %w", len(rows), err)
	}
	return nil
}

// Claim leases pending entries to the caller, oldest first. The CTE locks the
// selected rows with SKIP LOCKED, so relays on other replicas claim different
// entries instead of waiting.
func (r *repository) Claim(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]outbox.Entry, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []eventEntity
	err := r.conn(ctx).Raw(`
WITH claimable AS (
	SELECT id
	FROM outbox.events
	WHERE published_at IS NULL
	  AND dead_lettered_at IS NULL
	  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
	  AND (claimed_until IS NULL OR claimed_until <= ?)
	ORDER BY occurred_at, id
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
UPDATE outbox.events AS e
SET claimed_until = ?
FROM claimable
WHERE e.id = claimable.id
RETURNING e.id, e.type, e.organization_id, e.aggregate_type, e.aggregate_id,
          e.correlation_id, e.causation_id, e.occurred_at, e.payload, e.recorded_at,
          e.published_at, e.attempts, e.next_attempt_at, e.claimed_until,
          e.dead_lettered_at, e.last_error`,
		now, now, limit, now.Add(lease)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	entries := make([]outbox.Entry, len(rows))
	for i, row := range rows {
		entries[i] = row.toEntry()
	}
	return entries, nil
}

// MarkPublished records that the entries were dispatched and releases their
// leases.
func (r *repository) MarkPublished(ctx context.Context, now time.Time, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	err := r.conn(ctx).Model(&eventEntity{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"published_at":    now,
			"claimed_until":   nil,
			"next_attempt_at": nil,
		}).Error
	if err != nil {
		return fmt.Errorf("mark %d outbox event(s) published: %w", len(ids), err)
	}
	return nil
}

// MarkFailed records a failed delivery and when the event may be claimed
// again.
func (r *repository) MarkFailed(ctx context.Context, now time.Time, id string, retryAt time.Time, cause error) error {
	err := r.conn(ctx).Model(&eventEntity{}).
		Where("id = ? AND published_at IS NULL AND dead_lettered_at IS NULL", id).
		Updates(map[string]any{
			"attempts":        gorm.Expr("attempts + 1"),
			"last_error":      errorText(cause),
			"next_attempt_at": retryAt,
			"claimed_until":   nil,
		}).Error
	if err != nil {
		return fmt.Errorf("record failure of outbox event %s: %w", id, err)
	}
	return nil
}

// DeadLetter records that an event exhausted its retries.
func (r *repository) DeadLetter(ctx context.Context, now time.Time, id string, cause error) error {
	err := r.conn(ctx).Model(&eventEntity{}).
		Where("id = ? AND published_at IS NULL AND dead_lettered_at IS NULL", id).
		Updates(map[string]any{
			"attempts":         gorm.Expr("attempts + 1"),
			"last_error":       errorText(cause),
			"dead_lettered_at": now,
			"next_attempt_at":  nil,
			"claimed_until":    nil,
		}).Error
	if err != nil {
		return fmt.Errorf("dead-letter outbox event %s: %w", id, err)
	}
	return nil
}

// PurgePublished deletes entries published before the cutoff.
func (r *repository) PurgePublished(ctx context.Context, before time.Time) (int64, error) {
	res := r.conn(ctx).
		Where("published_at IS NOT NULL AND published_at < ?", before).
		Delete(&eventEntity{})
	if res.Error != nil {
		return 0, fmt.Errorf("purge published outbox events: %w", res.Error)
	}
	return res.RowsAffected, nil
}

func toEntity(e events.Envelope, recordedAt time.Time) eventEntity {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	row := eventEntity{
		ID:             e.ID,
		Type:           e.Type,
		OrganizationID: e.OrganizationID,
		AggregateType:  e.AggregateType,
		AggregateID:    e.AggregateID,
		CorrelationID:  e.CorrelationID,
		OccurredAt:     e.OccurredAt,
		Payload:        payload,
		RecordedAt:     recordedAt,
	}
	if e.CausationID != "" {
		causation := e.CausationID
		row.CausationID = &causation
	}
	return row
}

func (row eventEntity) toEntry() outbox.Entry {
	e := events.Envelope{
		ID:             row.ID,
		Type:           row.Type,
		OrganizationID: row.OrganizationID,
		AggregateType:  row.AggregateType,
		AggregateID:    row.AggregateID,
		OccurredAt:     row.OccurredAt.UTC(),
		CorrelationID:  row.CorrelationID,
		Payload:        json.RawMessage(row.Payload),
	}
	if row.CausationID != nil {
		e.CausationID = *row.CausationID
	}
	return outbox.Entry{Envelope: e, Attempts: row.Attempts}
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

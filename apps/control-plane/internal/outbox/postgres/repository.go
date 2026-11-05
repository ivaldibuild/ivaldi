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
// entries instead of waiting. RETURNING does not keep the CTE's order, so the
// outer SELECT sorts the claimed rows again.
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
), claimed AS (
	UPDATE outbox.events AS e
	SET claimed_until = ?
	FROM claimable
	WHERE e.id = claimable.id
	RETURNING e.id, e.type, e.organization_id, e.aggregate_type, e.aggregate_id,
	          e.correlation_id, e.causation_id, e.occurred_at, e.payload, e.recorded_at,
	          e.published_at, e.attempts, e.next_attempt_at, e.claimed_until,
	          e.dead_lettered_at, e.last_error
)
SELECT * FROM claimed ORDER BY occurred_at, id`,
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
// leases. Entries no longer held by the claim that returned them are left as
// they are, and reported with ErrLeaseLost.
func (r *repository) MarkPublished(ctx context.Context, now time.Time, entries []outbox.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	res := held(r.conn(ctx), entries...).
		Updates(map[string]any{
			"published_at":    now,
			"claimed_until":   nil,
			"next_attempt_at": nil,
		})
	if res.Error != nil {
		return fmt.Errorf("mark %d outbox event(s) published: %w", len(entries), res.Error)
	}
	if lost := int64(len(entries)) - res.RowsAffected; lost > 0 {
		return fmt.Errorf("mark outbox events published: %w (%d of %d)", outbox.ErrLeaseLost, lost, len(entries))
	}
	return nil
}

// MarkFailed records a failed delivery and when the event may be claimed
// again.
func (r *repository) MarkFailed(ctx context.Context, now time.Time, e outbox.Entry, retryAt time.Time, cause error) error {
	res := held(r.conn(ctx), e).
		Updates(map[string]any{
			"attempts":        gorm.Expr("attempts + 1"),
			"last_error":      errorText(cause),
			"next_attempt_at": retryAt,
			"claimed_until":   nil,
		})
	if res.Error != nil {
		return fmt.Errorf("record failure of outbox event %s: %w", e.Envelope.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("record failure of outbox event %s: %w", e.Envelope.ID, outbox.ErrLeaseLost)
	}
	return nil
}

// DeadLetter records that an event exhausted its retries.
func (r *repository) DeadLetter(ctx context.Context, now time.Time, e outbox.Entry, cause error) error {
	res := held(r.conn(ctx), e).
		Updates(map[string]any{
			"attempts":         gorm.Expr("attempts + 1"),
			"last_error":       errorText(cause),
			"dead_lettered_at": now,
			"next_attempt_at":  nil,
			"claimed_until":    nil,
		})
	if res.Error != nil {
		return fmt.Errorf("dead-letter outbox event %s: %w", e.Envelope.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("dead-letter outbox event %s: %w", e.Envelope.ID, outbox.ErrLeaseLost)
	}
	return nil
}

// held selects the entries still leased by the claims that returned them.
// Every mark clears claimed_until, and a later claim sets a later one, so a
// match also means the entry is neither published nor dead-lettered.
func held(db *gorm.DB, entries ...outbox.Entry) *gorm.DB {
	claims := make([][]any, len(entries))
	for i, e := range entries {
		claims[i] = []any{e.Envelope.ID, e.ClaimedUntil}
	}
	return db.Model(&eventEntity{}).Where("(id, claimed_until) IN ?", claims)
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
		ID:            e.ID,
		Type:          e.Type,
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID,
		CorrelationID: e.CorrelationID,
		OccurredAt:    e.OccurredAt,
		Payload:       payload,
		RecordedAt:    recordedAt,
	}
	// A platform-scoped event, such as one an Account raised, belongs to no
	// Organization.
	if e.OrganizationID != "" {
		org := e.OrganizationID
		row.OrganizationID = &org
	}
	if e.CausationID != "" {
		causation := e.CausationID
		row.CausationID = &causation
	}
	return row
}

func (row eventEntity) toEntry() outbox.Entry {
	e := events.Envelope{
		ID:            row.ID,
		Type:          row.Type,
		AggregateType: row.AggregateType,
		AggregateID:   row.AggregateID,
		OccurredAt:    row.OccurredAt.UTC(),
		CorrelationID: row.CorrelationID,
		Payload:       json.RawMessage(row.Payload),
	}
	if row.OrganizationID != nil {
		e.OrganizationID = *row.OrganizationID
	}
	if row.CausationID != nil {
		e.CausationID = *row.CausationID
	}
	entry := outbox.Entry{Envelope: e, Attempts: row.Attempts}
	if row.ClaimedUntil != nil {
		entry.ClaimedUntil = row.ClaimedUntil.UTC()
	}
	return entry
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

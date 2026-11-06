// Package postgres stores audit records in PostgreSQL.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// recordPrefix starts every audit record's ID.
const recordPrefix = "aud"

func init() { id.MustRegister(recordPrefix, "audit.records") }

type recordEntity struct {
	ID                    string
	OrganizationID        string
	Type                  string
	ResourceID            string
	ActorPrincipalID      *string
	ActorKind             string
	ActorSessionID        *string
	DecisionID            *string
	ApprovedByPrincipalID *string
	Before                []byte `gorm:"type:jsonb"`
	After                 []byte `gorm:"type:jsonb"`
	OccurredAt            time.Time
	RecordedAt            time.Time
}

func (recordEntity) TableName() string { return "audit.records" }

type recorder struct {
	db *gorm.DB
}

// NewRecorder returns the PostgreSQL audit Recorder. It writes through the
// transaction carried by the context, so a record commits with its change.
func NewRecorder(db *gorm.DB) audit.Recorder {
	return &recorder{db: db}
}

// Record inserts e. It refuses a record that does not say what happened, in
// which Organization, to what, or by whom; only the system actor may have no
// principal.
func (r *recorder) Record(ctx context.Context, e audit.Event) error {
	if err := check(e); err != nil {
		return err
	}
	before, err := snapshot(e.Before)
	if err != nil {
		return fmt.Errorf("audit %s: before: %w", e.Type, err)
	}
	after, err := snapshot(e.After)
	if err != nil {
		return fmt.Errorf("audit %s: after: %w", e.Type, err)
	}
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = clock.Now()
	}
	row := &recordEntity{
		ID:                    id.New(recordPrefix),
		OrganizationID:        e.OrganizationID,
		Type:                  e.Type,
		ResourceID:            e.ResourceID,
		ActorPrincipalID:      optional(e.Actor.PrincipalID),
		ActorKind:             string(e.Actor.Kind),
		ActorSessionID:        optional(e.Actor.SessionID),
		DecisionID:            optional(e.DecisionID),
		ApprovedByPrincipalID: optional(e.ApprovedByPrincipalID),
		Before:                before,
		After:                 after,
		OccurredAt:            occurred.UTC(),
		RecordedAt:            clock.Now(),
	}
	if err := database.Conn(ctx, r.db).Create(row).Error; err != nil {
		return fmt.Errorf("audit %s: insert: %w", e.Type, err)
	}
	return nil
}

func check(e audit.Event) error {
	var missing []error
	for _, f := range []struct{ name, value string }{
		{"type", e.Type},
		{"organization ID", e.OrganizationID},
		{"resource ID", e.ResourceID},
		{"actor kind", string(e.Actor.Kind)},
	} {
		if f.value == "" {
			missing = append(missing, fmt.Errorf("%s is required", f.name))
		}
	}
	// Only Ivaldi itself acts without a principal.
	if e.Actor.PrincipalID == "" && !e.Actor.IsSystem() {
		missing = append(missing, errors.New("actor principal ID is required"))
	}
	if len(missing) > 0 {
		return fmt.Errorf("audit %q: %w", e.Type, errors.Join(missing...))
	}
	return nil
}

// snapshot encodes a snapshot as JSON, or nil for none.
func snapshot(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

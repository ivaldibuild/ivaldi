package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const org = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"

type widget struct {
	Name string `json:"name"`
}

type row struct {
	ID, Type, ResourceID, ActorKind              string
	ActorPrincipalID, ActorSessionID, DecisionID *string
	Before, After                                *string
	OccurredAt                                   time.Time
}

func rows(t *testing.T, db *gorm.DB) []row {
	t.Helper()
	var out []row
	require.NoError(t, db.Raw(`SELECT id, type, resource_id, actor_principal_id, actor_kind, actor_session_id,
		decision_id, before::text AS before, after::text AS after, occurred_at FROM audit.records ORDER BY id`).Scan(&out).Error)
	return out
}

func event() audit.Event {
	return audit.Event{
		Type: "test.widget.renamed", OrganizationID: org, ResourceID: "wid_1",
		Actor:      identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser},
		DecisionID: "dcn_1", Before: widget{"cog"}, After: widget{"gear"},
		OccurredAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
	}
}

func TestRecord(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	rec := postgres.NewRecorder(db)

	require.NoError(t, rec.Record(context.Background(), event()))

	got := rows(t, db)
	require.Len(t, got, 1)
	r := got[0]
	assert.Regexp(t, `^aud_[0-9A-Z]{26}$`, r.ID)
	assert.Equal(t, "test.widget.renamed", r.Type)
	assert.Equal(t, "wid_1", r.ResourceID)
	assert.Equal(t, "prn_ada", *r.ActorPrincipalID)
	assert.Equal(t, "user", r.ActorKind)
	assert.Nil(t, r.ActorSessionID)
	assert.Equal(t, "dcn_1", *r.DecisionID)
	assert.JSONEq(t, `{"name":"cog"}`, *r.Before)
	assert.JSONEq(t, `{"name":"gear"}`, *r.After)
	assert.True(t, r.OccurredAt.Equal(event().OccurredAt))
}

func TestRecordWithoutBefore(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	e := event()
	e.Before = nil
	require.NoError(t, postgres.NewRecorder(db).Record(context.Background(), e))
	assert.Nil(t, rows(t, db)[0].Before)
}

func TestRecordRefusesAnIncompleteRecord(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	e := event()
	e.Actor = identity.Actor{}
	err := postgres.NewRecorder(db).Record(context.Background(), e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actor principal ID is required")
	assert.Empty(t, rows(t, db))
}

func TestRecordJoinsTheTransaction(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	rec := postgres.NewRecorder(db)
	boom := errors.New("the change failed")

	err := database.NewTransactor(db).WithinTx(context.Background(), func(ctx context.Context) error {
		require.NoError(t, rec.Record(ctx, event()))
		return boom
	})

	require.ErrorIs(t, err, boom)
	assert.Empty(t, rows(t, db), "the record rolls back with its change")
}

func TestRecordBySystem(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	e := event()
	e.Actor = identity.Actor{Kind: identity.ActorSystem}
	e.DecisionID = ""
	require.NoError(t, postgres.NewRecorder(db).Record(context.Background(), e))
	got := rows(t, db)
	require.Len(t, got, 1)
	assert.Nil(t, got[0].ActorPrincipalID)
	assert.Equal(t, "system", got[0].ActorKind)
	assert.Nil(t, got[0].DecisionID)
}

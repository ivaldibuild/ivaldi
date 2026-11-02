package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
)

const (
	testOrg = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testNow = "2026-11-02T09:00:00Z"
)

type widgetRenamed struct {
	WidgetID string `json:"widget_id"`
	Name     string `json:"name"`
}

func (widgetRenamed) EventName() string { return "test.v1.widget.renamed" }

type widgetArchived struct {
	WidgetID string `json:"widget_id"`
}

func (widgetArchived) EventName() string { return "test.v1.widget.archived" }

var testSource = events.Source{OrganizationID: testOrg, AggregateType: "test.Widget", AggregateID: "wid_01ARZ3NDEKTSV4RRFFQ69G5FAV"}

func TestRecordAndClaim(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	want := wrap(t,
		widgetRenamed{WidgetID: "wid_1", Name: "gear"},
		widgetArchived{WidgetID: "wid_1"},
	)

	require.NoError(t, repo.Record(context.Background(), want...))

	entries, err := repo.Claim(context.Background(), now, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	first := entries[0]
	assert.Equal(t, want[0].ID, first.Envelope.ID)
	assert.Equal(t, want[0].Type, first.Envelope.Type)
	assert.Equal(t, want[0].OrganizationID, first.Envelope.OrganizationID)
	assert.Equal(t, want[0].AggregateType, first.Envelope.AggregateType)
	assert.Equal(t, want[0].AggregateID, first.Envelope.AggregateID)
	assert.Equal(t, want[0].CorrelationID, first.Envelope.CorrelationID)
	assert.Empty(t, first.Envelope.CausationID)
	assert.Equal(t, want[0].OccurredAt, first.Envelope.OccurredAt)
	assert.JSONEq(t, string(want[0].Payload), string(first.Envelope.Payload))
	assert.Zero(t, first.Attempts)
	assert.Equal(t, want[1].ID, entries[1].Envelope.ID, "claim keeps recorded order")
}

func TestRecordIsIdempotent(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	env := wrap(t, widgetArchived{WidgetID: "wid_1"})

	require.NoError(t, repo.Record(context.Background(), env...))
	require.NoError(t, repo.Record(context.Background(), env...))

	entries, err := repo.Claim(context.Background(), now, 10, time.Minute)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a repeated event ID is stored once")
}

func TestRecordJoinsTheCallersTransaction(t *testing.T) {
	fixedClock(t)
	db := databasetest.New(t, migrations.FS)
	repo := postgres.NewRepository(db)
	env := wrap(t, widgetArchived{WidgetID: "wid_1"})
	rollback := errors.New("roll back")

	err := database.NewTransactor(db).WithinTx(context.Background(), func(ctx context.Context) error {
		if err := repo.Record(ctx, env...); err != nil {
			return err
		}
		return rollback
	})

	assert.ErrorIs(t, err, rollback)
	entries, err := repo.Claim(context.Background(), mustNow(t), 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, entries, "an event rolls back with the change that raised it")
}

func TestClaimHonorsItsLease(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(), wrap(t, widgetArchived{WidgetID: "wid_1"})...))

	claimed, err := repo.Claim(context.Background(), now, 1, 10*time.Second)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	again, err := repo.Claim(context.Background(), now.Add(5*time.Second), 1, 10*time.Second)
	require.NoError(t, err)
	assert.Empty(t, again, "a live lease keeps another relay off the event")

	after, err := repo.Claim(context.Background(), now.Add(11*time.Second), 1, 10*time.Second)
	require.NoError(t, err)
	assert.Len(t, after, 1, "an expired lease lets the event be reclaimed")
}

func TestMarkFailedSchedulesARetry(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(), wrap(t, widgetArchived{WidgetID: "wid_1"})...))
	claimed, err := repo.Claim(context.Background(), now, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, repo.MarkFailed(context.Background(), now, claimed[0].Envelope.ID, now.Add(time.Minute), errors.New("boom")))

	waiting, err := repo.Claim(context.Background(), now.Add(30*time.Second), 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, waiting, "the retry waits for its scheduled time")

	retried, err := repo.Claim(context.Background(), now.Add(time.Minute), 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, 1, retried[0].Attempts)
}

func TestDeadLetterStopsRetries(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(), wrap(t, widgetArchived{WidgetID: "wid_1"})...))
	claimed, err := repo.Claim(context.Background(), now, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, repo.DeadLetter(context.Background(), now, claimed[0].Envelope.ID, errors.New("gave up")))

	entries, err := repo.Claim(context.Background(), now.Add(1000*time.Hour), 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMarkPublishedRemovesTheEventFromClaims(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(), wrap(t, widgetArchived{WidgetID: "wid_1"})...))
	claimed, err := repo.Claim(context.Background(), now, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, repo.MarkPublished(context.Background(), now, []string{claimed[0].Envelope.ID}))

	entries, err := repo.Claim(context.Background(), now.Add(1000*time.Hour), 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestPurgePublishedKeepsPendingAndDeadLettered(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(),
		wrap(t, widgetArchived{WidgetID: "wid_1"})...))
	require.NoError(t, repo.Record(context.Background(),
		wrap(t, widgetRenamed{WidgetID: "wid_2", Name: "other"})...))
	claimed, err := repo.Claim(context.Background(), now, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	require.NoError(t, repo.MarkPublished(context.Background(), now, []string{claimed[0].Envelope.ID}))
	require.NoError(t, repo.DeadLetter(context.Background(), now, claimed[1].Envelope.ID, errors.New("gave up")))

	purged, err := repo.PurgePublished(context.Background(), now.Add(time.Second))
	require.NoError(t, err)
	assert.EqualValues(t, 1, purged)
}

func TestConcurrentClaimsDoNotOverlap(t *testing.T) {
	now := fixedClock(t)
	repo := newRepository(t)
	require.NoError(t, repo.Record(context.Background(), wrap(t,
		widgetRenamed{WidgetID: "wid_1", Name: "gear"},
		widgetArchived{WidgetID: "wid_2"},
	)...))

	var wg sync.WaitGroup
	results := make([][]events.Envelope, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entries, err := repo.Claim(context.Background(), now, 1, time.Minute)
			if err != nil {
				t.Errorf("claim %d: %v", i, err)
				return
			}
			for _, e := range entries {
				results[i] = append(results[i], e.Envelope)
			}
		}(i)
	}
	wg.Wait()

	ids := map[string]bool{}
	for _, got := range results {
		for _, e := range got {
			ids[e.ID] = true
		}
	}
	assert.Len(t, ids, 2, "each relay claims a different event")
}

func newRepository(t *testing.T) outbox.Store {
	t.Helper()
	return postgres.NewRepository(databasetest.New(t, migrations.FS))
}

func fixedClock(t *testing.T) time.Time {
	t.Helper()
	now, err := time.Parse(time.RFC3339, testNow)
	require.NoError(t, err)
	clock.Fix(t, now)
	return now
}

func mustNow(t *testing.T) time.Time {
	t.Helper()
	now, err := time.Parse(time.RFC3339, testNow)
	require.NoError(t, err)
	return now
}

func wrap(t *testing.T, evs ...aggregate.Event) []events.Envelope {
	t.Helper()
	envelopes, err := events.Wrap(context.Background(), testSource, evs...)
	require.NoError(t, err)
	return envelopes
}

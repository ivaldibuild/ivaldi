package redisstream

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

type widgetArchived struct {
	WidgetID string `json:"widget_id"`
}

func (widgetArchived) EventName() string { return "test.v1.widget.archived" }

func TestFields(t *testing.T) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 123456000, time.UTC)
	e := events.Envelope{
		ID:             "evt_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Type:           "test.v1.widget.archived",
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AggregateType:  "test.Widget",
		AggregateID:    "wid_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		OccurredAt:     at,
		CorrelationID:  "evt_correlation",
		CausationID:    "evt_cause",
		Payload:        json.RawMessage(`{"widget_id":"wid_1"}`),
	}

	got := fields(e)

	assert.Equal(t, e.ID, got["id"])
	assert.Equal(t, e.Type, got["type"])
	assert.Equal(t, e.OrganizationID, got["organization_id"])
	assert.Equal(t, e.AggregateType, got["aggregate_type"])
	assert.Equal(t, e.AggregateID, got["aggregate_id"])
	assert.Equal(t, "2026-11-02T09:00:00.123456Z", got["occurred_at"])
	assert.Equal(t, e.CorrelationID, got["correlation_id"])
	assert.Equal(t, e.CausationID, got["causation_id"])
	assert.Equal(t, string(e.Payload), got["payload"])
}

func TestNewPublisherRequiresAClientAndStream(t *testing.T) {
	tests := []struct {
		name string
		c    Config
		want string
	}{
		{"no client", Config{Stream: "events"}, "Client is required"},
		{"no stream", Config{Client: redis.NewClient(&redis.Options{Addr: "localhost:6379"})}, "Stream is required"},
		{"negative max length", Config{Client: redis.NewClient(&redis.Options{Addr: "localhost:6379"}), Stream: "events", MaxLen: -1}, "MaxLen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPublisher(tt.c)
			assert.Nil(t, p)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNewPublisherDefaultsTheMaxLength(t *testing.T) {
	p, err := NewPublisher(Config{
		Client: redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
		Stream: "events",
	})
	require.NoError(t, err)
	assert.Equal(t, DefaultMaxLen, p.maxLen)
}

func TestPublishToRedis(t *testing.T) {
	url := os.Getenv("IVALDI_TEST_REDIS_URL")
	if url == "" {
		t.Skip("set IVALDI_TEST_REDIS_URL to run Redis tests")
	}
	opt, err := redis.ParseURL(url)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	require.NoError(t, client.Ping(ctx).Err())

	stream := "ivaldi_test_" + id.ULID()
	t.Cleanup(func() { client.Del(context.Background(), stream) })
	clock.Fix(t, time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))

	p, err := NewPublisher(Config{Client: client, Stream: stream, MaxLen: 1000})
	require.NoError(t, err)
	envelopes, err := events.Wrap(ctx,
		events.Source{OrganizationID: "org_1", AggregateType: "test.Widget", AggregateID: "wid_1"},
		widgetArchived{WidgetID: "wid_1"},
	)
	require.NoError(t, err)

	require.NoError(t, p.Publish(ctx, envelopes...))

	msgs, err := client.XRange(ctx, stream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	got := msgs[0].Values
	assert.Equal(t, envelopes[0].ID, got["id"])
	assert.Equal(t, envelopes[0].Type, got["type"])
	assert.Equal(t, "org_1", got["organization_id"])
	assert.Equal(t, "test.Widget", got["aggregate_type"])
	assert.Equal(t, "wid_1", got["aggregate_id"])
	assert.Equal(t, "2026-11-02T09:00:00Z", got["occurred_at"])
	assert.Equal(t, envelopes[0].CorrelationID, got["correlation_id"])
	assert.Equal(t, "", got["causation_id"])
	assert.JSONEq(t, `{"widget_id":"wid_1"}`, got["payload"].(string))
}

func TestPublishIgnoresAnEmptyBatch(t *testing.T) {
	// A nil client would panic if Publish touched it.
	p := &Publisher{}
	assert.NoError(t, p.Publish(context.Background()))
}

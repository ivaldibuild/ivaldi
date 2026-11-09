package redisstream

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/events"
)

func TestEnvelopeFromFieldsRoundTrip(t *testing.T) {
	want := events.Envelope{
		ID:             "evt_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Type:           "test.v1.widget.archived",
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		AggregateType:  "test.Widget",
		AggregateID:    "wid_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		OccurredAt:     time.Date(2026, 11, 9, 9, 0, 0, 123456000, time.UTC),
		CorrelationID:  "evt_correlation",
		CausationID:    "evt_cause",
		Payload:        json.RawMessage(`{"widget_id":"wid_1"}`),
	}

	got, err := envelopeFromFields(fields(want))

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEnvelopeFromFieldsToleratesAbsentOptionalFields(t *testing.T) {
	got, err := envelopeFromFields(map[string]any{
		"id":          "evt_1",
		"type":        "test.v1.widget.archived",
		"occurred_at": "2026-11-09T09:00:00Z",
		"payload":     `{"widget_id":"wid_1"}`,
	})

	require.NoError(t, err)
	assert.Equal(t, "evt_1", got.ID)
	assert.Empty(t, got.OrganizationID)
	assert.Empty(t, got.AggregateType)
	assert.Empty(t, got.AggregateID)
	assert.Empty(t, got.CorrelationID)
	assert.Empty(t, got.CausationID)
}

func TestEnvelopeFromFieldsAcceptsBytes(t *testing.T) {
	got, err := envelopeFromFields(map[string]any{
		"id":          []byte("evt_1"),
		"type":        []byte("test.v1.widget.archived"),
		"occurred_at": []byte("2026-11-09T09:00:00Z"),
		"payload":     []byte(`{"widget_id":"wid_1"}`),
	})

	require.NoError(t, err)
	assert.Equal(t, "evt_1", got.ID)
	assert.Equal(t, "test.v1.widget.archived", got.Type)
	assert.JSONEq(t, `{"widget_id":"wid_1"}`, string(got.Payload))
}

func TestEnvelopeFromFieldsRejectsInvalidEntries(t *testing.T) {
	valid := fields(events.Envelope{
		ID:         "evt_1",
		Type:       "test.v1.widget.archived",
		OccurredAt: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC),
		Payload:    json.RawMessage(`{"widget_id":"wid_1"}`),
	})
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{"missing id", func(v map[string]any) { delete(v, "id") }, "id"},
		{"empty id", func(v map[string]any) { v["id"] = "" }, "id"},
		{"missing type", func(v map[string]any) { delete(v, "type") }, "type"},
		{"missing occurred_at", func(v map[string]any) { delete(v, "occurred_at") }, "occurred_at"},
		{"bad occurred_at", func(v map[string]any) { v["occurred_at"] = "yesterday" }, "parse occurred_at"},
		{"missing payload", func(v map[string]any) { delete(v, "payload") }, "payload"},
		{"invalid payload", func(v map[string]any) { v["payload"] = "{" }, "not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := make(map[string]any, len(valid))
			for k, v := range valid {
				values[k] = v
			}
			tt.mutate(values)

			_, err := envelopeFromFields(values)

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

package redisstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// fields is the stream entry's flat contract. Keep it in step with
// schemas/event-envelope.v1.json and with envelopeFromFields.
func fields(e events.Envelope) map[string]any {
	return map[string]any{
		"id":              e.ID,
		"type":            e.Type,
		"organization_id": e.OrganizationID,
		"aggregate_type":  e.AggregateType,
		"aggregate_id":    e.AggregateID,
		"occurred_at":     e.OccurredAt.UTC().Format(time.RFC3339Nano),
		"correlation_id":  e.CorrelationID,
		"causation_id":    e.CausationID,
		"payload":         string(e.Payload),
	}
}

// envelopeFromFields decodes the contract that fields writes. It requires the
// fields a consumer cannot do without and tolerates absent optional fields, so
// a consumer can read an entry from an older or newer publisher.
func envelopeFromFields(values map[string]any) (events.Envelope, error) {
	var e events.Envelope
	var ok bool

	if e.ID, ok = stringField(values, "id"); !ok || e.ID == "" {
		return e, errors.New("missing or invalid id")
	}
	if e.Type, ok = stringField(values, "type"); !ok || e.Type == "" {
		return e, errors.New("missing or invalid type")
	}
	occurredAt, ok := stringField(values, "occurred_at")
	if !ok || occurredAt == "" {
		return e, errors.New("missing or invalid occurred_at")
	}
	at, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return e, fmt.Errorf("parse occurred_at %q: %w", occurredAt, err)
	}
	e.OccurredAt = at.UTC()

	payload, ok := stringField(values, "payload")
	if !ok || payload == "" {
		return e, errors.New("missing or invalid payload")
	}
	if !json.Valid([]byte(payload)) {
		return e, errors.New("payload is not valid JSON")
	}
	e.Payload = json.RawMessage(payload)

	e.OrganizationID, _ = stringField(values, "organization_id")
	e.AggregateType, _ = stringField(values, "aggregate_type")
	e.AggregateID, _ = stringField(values, "aggregate_id")
	e.CorrelationID, _ = stringField(values, "correlation_id")
	e.CausationID, _ = stringField(values, "causation_id")
	return e, nil
}

// stringField returns values[key] as a string. Redis returns bulk strings as
// strings, but a client may hand back bytes, so both are accepted.
func stringField(values map[string]any, key string) (string, bool) {
	switch v := values[key].(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}

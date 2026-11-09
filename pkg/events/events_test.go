package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

type widgetRenamed struct {
	WidgetID string `json:"widget_id"`
	Name     string `json:"name"`
}

func (widgetRenamed) EventName() string { return "test.widget.renamed.v1" }

type widgetArchived struct {
	WidgetID string `json:"widget_id"`
}

func (widgetArchived) EventName() string { return "test.widget.archived.v1" }

type unencodable struct {
	Ch chan int
}

func (unencodable) EventName() string { return "test.unencodable.v1" }

var widget = events.Source{OrganizationID: "org_1", AggregateType: "test.Widget", AggregateID: "wid_1"}

func wrap(t *testing.T, ctx context.Context, e ...aggregate.Event) []events.Envelope {
	t.Helper()
	envelopes, err := events.Wrap(ctx, widget, e...)
	require.NoError(t, err)
	return envelopes
}

func TestWrap(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	clock.Fix(t, at)

	envelopes := wrap(t, context.Background(),
		widgetRenamed{WidgetID: "wid_1", Name: "gear"},
		widgetArchived{WidgetID: "wid_1"},
	)

	require.Len(t, envelopes, 2)
	first := envelopes[0]
	assert.True(t, id.Valid("evt", first.ID), "event IDs are prefixed: %s", first.ID)
	assert.Equal(t, "test.widget.renamed.v1", first.Type)
	assert.Equal(t, "org_1", first.OrganizationID)
	assert.Equal(t, "test.Widget", first.AggregateType)
	assert.Equal(t, "wid_1", first.AggregateID)
	assert.Equal(t, at, first.OccurredAt)
	assert.JSONEq(t, `{"widget_id":"wid_1","name":"gear"}`, string(first.Payload))
	assert.Equal(t, "test.widget.archived.v1", envelopes[1].Type, "events keep their order")
	assert.NotEqual(t, first.ID, envelopes[1].ID)
}

func TestWrapStartsAChainWithoutACause(t *testing.T) {
	e := wrap(t, context.Background(), widgetArchived{})[0]

	assert.Equal(t, e.ID, e.CorrelationID)
	assert.Empty(t, e.CausationID)
}

func TestWrapContinuesTheChainOfItsCause(t *testing.T) {
	cause := events.Envelope{ID: "evt_cause", CorrelationID: "evt_origin"}

	e := wrap(t, events.WithCause(context.Background(), cause), widgetArchived{})[0]

	assert.Equal(t, "evt_origin", e.CorrelationID)
	assert.Equal(t, "evt_cause", e.CausationID)
}

func TestWrapFailsOnAnUnencodableEvent(t *testing.T) {
	_, err := events.Wrap(context.Background(), widget, unencodable{Ch: make(chan int)})

	assert.ErrorContains(t, err, "test.unencodable.v1")
}

func TestDecode(t *testing.T) {
	renamed := wrap(t, context.Background(), widgetRenamed{WidgetID: "wid_1", Name: "gear"})[0]

	tests := []struct {
		name    string
		e       events.Envelope
		wantErr string
	}{
		{name: "matching type", e: renamed},
		{name: "other type", e: events.Envelope{ID: "evt_1", Type: "test.widget.archived.v1"}, wantErr: "not a test.widget.renamed.v1"},
		{name: "bad payload", e: events.Envelope{ID: "evt_1", Type: "test.widget.renamed.v1", Payload: json.RawMessage(`[]`)}, wantErr: "decode event evt_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := events.Decode[widgetRenamed](tt.e)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, widgetRenamed{WidgetID: "wid_1", Name: "gear"}, got)
		})
	}
}

// recorder is a handler that notes the types of the events it handles.
type recorder struct {
	handled []string
	err     error
}

func (r *recorder) Handle(_ context.Context, e events.Envelope) error {
	r.handled = append(r.handled, e.Type)
	return r.err
}

func TestRouterSendsEventsToTheirSubscribers(t *testing.T) {
	envelopes := wrap(t, context.Background(), widgetRenamed{}, widgetArchived{})
	var r events.Router
	renames, both := &recorder{}, &recorder{}
	r.Subscribe(renames, "test.widget.renamed.v1")
	r.Subscribe(both, "test.widget.renamed.v1", "test.widget.archived.v1")

	for _, e := range envelopes {
		require.NoError(t, r.Dispatch(context.Background(), e))
	}

	assert.Equal(t, []string{"test.widget.renamed.v1"}, renames.handled)
	assert.Equal(t, []string{"test.widget.renamed.v1", "test.widget.archived.v1"}, both.handled)
}

func TestRouterIsAHandler(t *testing.T) {
	envelopes := wrap(t, context.Background(), widgetRenamed{})
	var r events.Router
	renames := &recorder{}
	r.Subscribe(renames, "test.widget.renamed.v1")

	var h events.Handler = &r
	require.NoError(t, h.Handle(context.Background(), envelopes[0]))

	assert.Equal(t, []string{"test.widget.renamed.v1"}, renames.handled)
}

func TestRouterIgnoresEventsWithoutSubscribers(t *testing.T) {
	var r events.Router

	assert.NoError(t, r.Dispatch(context.Background(), events.Envelope{Type: "test.unknown.v1"}))
}

func TestRouterRunsEveryHandlerAndJoinsTheirErrors(t *testing.T) {
	errFirst, errLast := errors.New("first failed"), errors.New("last failed")
	first, middle, last := &recorder{err: errFirst}, &recorder{}, &recorder{err: errLast}
	var r events.Router
	r.Subscribe(first, "test.widget.archived.v1")
	r.Subscribe(middle, "test.widget.archived.v1")
	r.Subscribe(last, "test.widget.archived.v1")

	err := r.Dispatch(context.Background(), events.Envelope{ID: "evt_1", Type: "test.widget.archived.v1"})

	assert.ErrorIs(t, err, errFirst)
	assert.ErrorIs(t, err, errLast)
	assert.ErrorContains(t, err, "evt_1")
	assert.Len(t, middle.handled, 1, "a failing handler does not stop the others")
	assert.Len(t, last.handled, 1)
}

func TestOnDecodesThePayload(t *testing.T) {
	e := wrap(t, context.Background(), widgetRenamed{WidgetID: "wid_1", Name: "gear"})[0]
	var r events.Router
	var got widgetRenamed
	events.On(&r, func(_ context.Context, env events.Envelope, event widgetRenamed) error {
		assert.Equal(t, e.ID, env.ID)
		got = event
		return nil
	})

	require.NoError(t, r.Dispatch(context.Background(), e))

	assert.Equal(t, widgetRenamed{WidgetID: "wid_1", Name: "gear"}, got)
}

func TestOnFailsOnAPayloadItCannotDecode(t *testing.T) {
	var r events.Router
	called := false
	events.On(&r, func(context.Context, events.Envelope, widgetRenamed) error {
		called = true
		return nil
	})

	err := r.Dispatch(context.Background(), events.Envelope{ID: "evt_1", Type: "test.widget.renamed.v1", Payload: json.RawMessage(`[]`)})

	assert.ErrorContains(t, err, "decode event evt_1")
	assert.False(t, called)
}

func TestHandlersRaiseEventsInTheChainOfTheEventTheyHandle(t *testing.T) {
	origin := wrap(t, context.Background(), widgetRenamed{WidgetID: "wid_1"})[0]
	var r events.Router
	var raised events.Envelope
	events.On(&r, func(ctx context.Context, _ events.Envelope, _ widgetRenamed) error {
		raised = wrap(t, ctx, widgetArchived{WidgetID: "wid_1"})[0]
		return nil
	})

	require.NoError(t, r.Dispatch(context.Background(), origin))

	assert.Equal(t, origin.ID, raised.CausationID)
	assert.Equal(t, origin.ID, raised.CorrelationID)
}

func TestSubscribeWithoutTypesPanics(t *testing.T) {
	var r events.Router

	assert.Panics(t, func() { r.Subscribe(&recorder{}) })
}

type publisher struct {
	published []events.Envelope
}

func (p *publisher) Publish(_ context.Context, envelopes ...events.Envelope) error {
	p.published = append(p.published, envelopes...)
	return nil
}

func TestForwardPublishesHandledEvents(t *testing.T) {
	e := wrap(t, context.Background(), widgetArchived{WidgetID: "wid_1"})[0]
	p := &publisher{}
	var r events.Router
	r.Subscribe(events.Forward(p), "test.widget.archived.v1")

	require.NoError(t, r.Dispatch(context.Background(), e))

	assert.Equal(t, []events.Envelope{e}, p.published)
}

// testDelivery is a Delivery for exercising Router.HandleDelivery.
type testDelivery struct {
	envelope events.Envelope
	acked    bool
}

func (d *testDelivery) Envelope() events.Envelope { return d.envelope }

func (d *testDelivery) EntryID() string { return "1-0" }

func (d *testDelivery) DeliveryCount() int64 { return 1 }

func (d *testDelivery) Ack(context.Context) error {
	d.acked = true
	return nil
}

func TestRouterHandlesDeliveries(t *testing.T) {
	e := wrap(t, context.Background(), widgetRenamed{WidgetID: "wid_1"})[0]
	var r events.Router
	renames := &recorder{}
	r.Subscribe(renames, "test.widget.renamed.v1")
	d := &testDelivery{envelope: e}

	require.NoError(t, r.HandleDelivery(context.Background(), d))

	assert.Equal(t, []string{"test.widget.renamed.v1"}, renames.handled)
	assert.False(t, d.acked, "the router leaves acknowledgement to the consumer")
}

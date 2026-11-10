package redisstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func validConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		Client:           redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
		Stream:           "events",
		Group:            "group",
		Consumer:         "consumer",
		Handler:          events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error { return nil }),
		DeadLetterStream: "events:dead",
	}
}

func TestNewConsumerRequiresSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ConsumerConfig)
		want   string
	}{
		{"no client", func(c *ConsumerConfig) { c.Client = nil }, "Client is required"},
		{"no stream", func(c *ConsumerConfig) { c.Stream = "" }, "Stream is required"},
		{"no group", func(c *ConsumerConfig) { c.Group = "" }, "Group is required"},
		{"no consumer", func(c *ConsumerConfig) { c.Consumer = "" }, "Consumer is required"},
		{"no handler", func(c *ConsumerConfig) { c.Handler = nil }, "Handler is required"},
		{"no dead letter stream", func(c *ConsumerConfig) { c.DeadLetterStream = "" }, "DeadLetterStream is required"},
		{"dead letter stream is the source", func(c *ConsumerConfig) { c.DeadLetterStream = c.Stream }, "must differ"},
		{"negative batch", func(c *ConsumerConfig) { c.Batch = -1 }, "Batch"},
		{"negative concurrency", func(c *ConsumerConfig) { c.Concurrency = -1 }, "Concurrency"},
		{"negative block timeout", func(c *ConsumerConfig) { c.BlockTimeout = -1 }, "BlockTimeout"},
		{"negative claim idle timeout", func(c *ConsumerConfig) { c.ClaimIdleTimeout = -1 }, "ClaimIdleTimeout"},
		{"negative max deliveries", func(c *ConsumerConfig) { c.MaxDeliveries = -1 }, "MaxDeliveries"},
		{"negative retry interval", func(c *ConsumerConfig) { c.RetryInterval = -1 }, "RetryInterval"},
		{"negative dead letter max length", func(c *ConsumerConfig) { c.DeadLetterMaxLen = -1 }, "DeadLetterMaxLen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConsumerConfig()
			tt.mutate(&cfg)

			c, err := NewConsumer(cfg)

			assert.Nil(t, c)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNewConsumerDefaults(t *testing.T) {
	c, err := NewConsumer(validConsumerConfig())
	require.NoError(t, err)

	assert.Equal(t, "0", c.start)
	assert.Equal(t, DefaultBatch, c.batch)
	assert.Equal(t, DefaultConcurrency, c.concurrency)
	assert.Equal(t, DefaultBlockTimeout, c.blockTimeout)
	assert.Equal(t, DefaultClaimIdleTimeout, c.claimIdle)
	assert.Equal(t, DefaultMaxDeliveries, c.maxDeliveries)
	assert.Equal(t, DefaultRetryInterval, c.retryInterval)
	assert.Zero(t, c.deadLetterMaxLen)
}

func TestNextStreamID(t *testing.T) {
	tests := []struct {
		id      string
		want    string
		wantErr string
	}{
		{id: "1-2", want: "1-3"},
		{id: "1-18446744073709551615", want: "2-0"},
		{id: "not-an-id", wantErr: "invalid stream ID"},
		{id: "x-1", wantErr: "invalid stream ID"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, err := nextStreamID(tt.id)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeliveryAckIsIdempotent(t *testing.T) {
	var calls int
	d := &delivery{ack: func(context.Context) error {
		calls++
		return nil
	}}

	require.NoError(t, d.Ack(context.Background()))
	require.NoError(t, d.Ack(context.Background()))

	assert.Equal(t, 1, calls, "a second acknowledgement changes nothing")
	assert.True(t, d.isAcked())
}

func TestDeliveryAckRetriesAfterFailure(t *testing.T) {
	want := errors.New("redis unavailable")
	calls := 0
	d := &delivery{ack: func(context.Context) error {
		calls++
		if calls == 1 {
			return want
		}
		return nil
	}}

	assert.ErrorIs(t, d.Ack(context.Background()), want)
	assert.False(t, d.isAcked())
	require.NoError(t, d.Ack(context.Background()))
	assert.True(t, d.isAcked())
	assert.Equal(t, 2, calls)
}

func TestDeadLetterValues(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	clock.Fix(t, at)
	values := map[string]any{
		"id":      "evt_1",
		"type":    "test.v1.widget.archived",
		"payload": `{"widget_id":"wid_1"}`,
	}

	got := deadLetterValues(values, "1700000000000-0", "events", "control-plane", "cp-0", 5, errors.New("boom"))

	assert.Equal(t, "evt_1", got["id"], "the original contract fields are kept")
	assert.Equal(t, "test.v1.widget.archived", got["type"])
	assert.Equal(t, `{"widget_id":"wid_1"}`, got["payload"])
	assert.Equal(t, "boom", got["error"])
	assert.Equal(t, at.Format(time.RFC3339Nano), got["failed_at"])
	assert.Equal(t, "5", got["deliveries"])
	assert.Equal(t, "events", got["source_stream"])
	assert.Equal(t, "1700000000000-0", got["source_entry_id"])
	assert.Equal(t, "control-plane", got["consumer_group"])
	assert.Equal(t, "cp-0", got["consumer"])
}

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("IVALDI_TEST_REDIS_URL")
	if url == "" {
		t.Skip("set IVALDI_TEST_REDIS_URL to run Redis tests")
	}
	opt, err := redis.ParseURL(url)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(context.Background()).Err())
	return client
}

func testStreamNames(t *testing.T, client *redis.Client) (stream, group, dead string) {
	t.Helper()
	stream = "ivaldi_test_" + id.ULID()
	group = stream + ":group"
	dead = stream + ":dead"
	t.Cleanup(func() {
		ctx := context.Background()
		_ = client.Del(ctx, stream, dead).Err()
	})
	return stream, group, dead
}

func testEnvelope(t *testing.T, widgetID string) events.Envelope {
	t.Helper()
	envelopes, err := events.Wrap(context.Background(), events.Source{
		OrganizationID: "org_1",
		AggregateType:  "test.Widget",
		AggregateID:    widgetID,
	}, widgetArchived{WidgetID: widgetID})
	require.NoError(t, err)
	return envelopes[0]
}

func publishEnvelope(t *testing.T, client *redis.Client, stream string, e events.Envelope) {
	t.Helper()
	_, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: fields(e),
	}).Result()
	require.NoError(t, err)
}

func newTestConsumer(t *testing.T, client *redis.Client, stream, group, consumer, dead string, handler events.DeliveryHandler, tune ...func(*ConsumerConfig)) *Consumer {
	t.Helper()
	cfg := ConsumerConfig{
		Client:           client,
		Stream:           stream,
		Group:            group,
		Consumer:         consumer,
		Handler:          handler,
		DeadLetterStream: dead,
		Batch:            10,
		Concurrency:      4,
		BlockTimeout:     50 * time.Millisecond,
		ClaimIdleTimeout: 200 * time.Millisecond,
		MaxDeliveries:    5,
		RetryInterval:    50 * time.Millisecond,
	}
	for _, f := range tune {
		f(&cfg)
	}
	c, err := NewConsumer(cfg)
	require.NoError(t, err)
	return c
}

func runConsumer(t *testing.T, c *Consumer) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Error("consumer did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func pendingCount(ctx context.Context, client *redis.Client, stream, group string) (int64, error) {
	p, err := client.XPending(ctx, stream, group).Result()
	if err != nil {
		return 0, err
	}
	return p.Count, nil
}

func requirePending(t *testing.T, client *redis.Client, stream, group string, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		n, err := pendingCount(context.Background(), client, stream, group)
		return err == nil && n == want
	}, 3*time.Second, 10*time.Millisecond, "pending entries should be %d", want)
}

func requireStreamLen(t *testing.T, client *redis.Client, stream string, want int64) []redis.XMessage {
	t.Helper()
	var msgs []redis.XMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = client.XRange(context.Background(), stream, "-", "+").Result()
		return err == nil && int64(len(msgs)) == want
	}, 3*time.Second, 10*time.Millisecond, "stream %s should have %d entries", stream, want)
	return msgs
}

func TestConsumerDispatchesAndAcknowledges(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	envelope := testEnvelope(t, "wid_1")
	publishEnvelope(t, client, stream, envelope)

	received := make(chan events.Envelope, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(_ context.Context, d events.Delivery) error {
			received <- d.Envelope()
			return nil
		}))
	stop := runConsumer(t, c)

	select {
	case got := <-received:
		assert.Equal(t, envelope.ID, got.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not receive the event")
	}
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerLeavesAFailedDeliveryPending(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	handled := make(chan struct{}, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled <- struct{}{}
			return errors.New("boom")
		}), func(c *ConsumerConfig) {
			c.ClaimIdleTimeout = 5 * time.Second
		})
	stop := runConsumer(t, c)

	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not handle the event")
	}
	time.Sleep(100 * time.Millisecond)
	requirePending(t, client, stream, group, 1)
	stop()
}

func TestConsumerDrainsItsOwnPendingOnStartup(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())

	envelope := testEnvelope(t, "wid_1")
	publishEnvelope(t, client, stream, envelope)

	// Simulate a previous run that read the entry and died before acking.
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "replica-1",
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, read, 1)

	received := make(chan events.Delivery, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(_ context.Context, d events.Delivery) error {
			received <- d
			return nil
		}))
	stop := runConsumer(t, c)

	select {
	case d := <-received:
		assert.Equal(t, envelope.ID, d.Envelope().ID)
		assert.Equal(t, int64(2), d.DeliveryCount(), "the startup claim is the second delivery")
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not drain its pending entry")
	}
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerReclaimsAnIdleEntryFromAnotherConsumer(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())

	envelope := testEnvelope(t, "wid_1")
	publishEnvelope(t, client, stream, envelope)

	// A consumer that read the entry and never acknowledged it.
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "gone",
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, read, 1)

	received := make(chan events.Delivery, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(_ context.Context, d events.Delivery) error {
			received <- d
			return nil
		}), func(c *ConsumerConfig) {
			c.ClaimIdleTimeout = 100 * time.Millisecond
		})
	stop := runConsumer(t, c)

	select {
	case d := <-received:
		assert.Equal(t, envelope.ID, d.Envelope().ID)
		assert.Equal(t, int64(2), d.DeliveryCount())
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not reclaim the idle entry")
	}
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerDeadLettersAfterMaxDeliveries(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	var handled atomic.Int64
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled.Add(1)
			return errors.New("boom")
		}), func(c *ConsumerConfig) {
			c.ClaimIdleTimeout = 100 * time.Millisecond
			c.MaxDeliveries = 2
		})
	stop := runConsumer(t, c)

	msgs := requireStreamLen(t, client, dead, 1)
	assert.GreaterOrEqual(t, handled.Load(), int64(2))
	assert.Equal(t, "boom", msgs[0].Values["error"])
	assert.Equal(t, "2", msgs[0].Values["deliveries"])
	assert.Equal(t, stream, msgs[0].Values["source_stream"])
	assert.Equal(t, group, msgs[0].Values["consumer_group"])
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerDeadLettersAPermanentFailureAtOnce(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	var handled atomic.Int64
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled.Add(1)
			return fmt.Errorf("%w: widget has no name", events.ErrPermanent)
		}), func(c *ConsumerConfig) {
			c.ClaimIdleTimeout = 100 * time.Millisecond
			c.MaxDeliveries = 5
		})
	stop := runConsumer(t, c)

	msgs := requireStreamLen(t, client, dead, 1)
	assert.Equal(t, int64(1), handled.Load())
	assert.Equal(t, "1", msgs[0].Values["deliveries"])
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerDeadLettersAPoisonEntry(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	ctx := context.Background()
	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{
			"id":      "evt_1",
			"type":    "test.v1.widget.archived",
			"payload": `{"widget_id":"wid_1"}`,
			// occurred_at is missing.
		},
	}).Result()
	require.NoError(t, err)

	var handled atomic.Int64
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled.Add(1)
			return nil
		}))
	stop := runConsumer(t, c)

	msgs := requireStreamLen(t, client, dead, 1)
	assert.Zero(t, handled.Load(), "a poison entry never reaches the handler")
	assert.Contains(t, msgs[0].Values["error"], "occurred_at")
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerManualAck(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	delivered := make(chan events.Delivery, 1)
	release := make(chan struct{})
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(ctx context.Context, d events.Delivery) error {
			delivered <- d
			<-release
			return d.Ack(ctx)
		}), func(c *ConsumerConfig) {
			c.ManualAck = true
			c.ClaimIdleTimeout = 5 * time.Second
		})
	stop := runConsumer(t, c)

	var d events.Delivery
	select {
	case d = <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not deliver the event")
	}
	requirePending(t, client, stream, group, 1)
	assert.NotEmpty(t, d.EntryID())

	close(release)
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerManualAckLeavesAnUnacknowledgedDeliveryPending(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	handled := make(chan struct{}, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled <- struct{}{}
			return nil
		}), func(c *ConsumerConfig) {
			c.ManualAck = true
			c.ClaimIdleTimeout = 5 * time.Second
		})
	stop := runConsumer(t, c)

	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not deliver the event")
	}
	time.Sleep(100 * time.Millisecond)
	requirePending(t, client, stream, group, 1)
	stop()
}

func TestConsumerDoesNotRetryAnAcknowledgedFailure(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	var handled atomic.Int64
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(ctx context.Context, d events.Delivery) error {
			handled.Add(1)
			if err := d.Ack(ctx); err != nil {
				return err
			}
			return errors.New("work committed but reporting failed")
		}), func(c *ConsumerConfig) {
			c.ManualAck = true
			c.ClaimIdleTimeout = 100 * time.Millisecond
		})
	stop := runConsumer(t, c)

	requirePending(t, client, stream, group, 0)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int64(1), handled.Load(), "an acknowledged delivery is not redelivered")
	assert.Zero(t, streamLen(t, client, dead), "an acknowledged delivery is not dead-lettered")
	stop()
}

func TestConsumerReadsABatchAndAcknowledgesEachMessage(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	const count = 5
	want := map[string]bool{}
	for i := 0; i < count; i++ {
		e := testEnvelope(t, "wid_"+strconv.Itoa(i))
		want[e.ID] = true
		publishEnvelope(t, client, stream, e)
	}

	var mu sync.Mutex
	got := map[string]bool{}
	done := make(chan struct{})
	var once sync.Once
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(_ context.Context, d events.Delivery) error {
			mu.Lock()
			defer mu.Unlock()
			got[d.Envelope().ID] = true
			if len(got) == count {
				once.Do(func() { close(done) })
			}
			return nil
		}), func(c *ConsumerConfig) {
			c.Batch = count
			c.Concurrency = count
		})
	stop := runConsumer(t, c)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not handle the whole batch")
	}
	mu.Lock()
	assert.Equal(t, want, got)
	mu.Unlock()
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerStartsTwiceInOneGroup(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	handler := events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error { return nil })

	first := newTestConsumer(t, client, stream, group, "replica-1", dead, handler)
	second := newTestConsumer(t, client, stream, group, "replica-2", dead, handler)
	stopFirst := runConsumer(t, first)
	stopSecond := runConsumer(t, second)

	time.Sleep(100 * time.Millisecond)
	stopSecond()
	stopFirst()
}

func TestConsumerProgress(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	handled := make(chan struct{}, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled <- struct{}{}
			return nil
		}))
	stop := runConsumer(t, c)

	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not handle the event")
	}
	requirePending(t, client, stream, group, 0)

	progress, err := c.Progress(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, progress.LastDeliveredID)
	assert.Zero(t, progress.Pending)
	assert.GreaterOrEqual(t, progress.Consumers, int64(1))
	stop()
}

func TestConsumerRequiresAnExistingGroupWhenCreationIsDisabled(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	ctx := context.Background()
	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"id": "evt_1"},
	}).Result()
	require.NoError(t, err)

	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error { return nil }),
		func(c *ConsumerConfig) {
			c.DisableGroupCreation = true
		})

	err = c.Run(ctx)

	assert.ErrorContains(t, err, "does not exist")
}

func TestConsumerSurvivesAPanickingHandler(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			panic("boom")
		}), func(c *ConsumerConfig) {
			c.ClaimIdleTimeout = 100 * time.Millisecond
			c.MaxDeliveries = 2
		})
	stop := runConsumer(t, c)

	msgs := requireStreamLen(t, client, dead, 1)
	assert.Equal(t, "handler panicked: boom", msgs[0].Values["error"])
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerDeadLettersAnEntryWhoseDeliveriesNeverFinished(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	// Simulate five runs that each read the entry and then crashed.
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "replica-1",
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	entryID := read[0].Messages[0].ID
	require.NoError(t, client.Do(ctx, "XCLAIM", stream, group, "replica-1", "0", entryID, "RETRYCOUNT", "5").Err())

	var handled atomic.Int64
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			handled.Add(1)
			return nil
		}))
	stop := runConsumer(t, c)

	msgs := requireStreamLen(t, client, dead, 1)
	assert.Zero(t, handled.Load(), "the entry is not handed to a handler that may crash again")
	assert.Equal(t, "no outcome after 5 deliveries", msgs[0].Values["error"])
	assert.Equal(t, entryID, msgs[0].Values["source_entry_id"])
	requirePending(t, client, stream, group, 0)
	stop()
}

func TestConsumerAcknowledgesAHandlerThatFinishesDuringShutdown(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	publishEnvelope(t, client, stream, testEnvelope(t, "wid_1"))

	started := make(chan struct{})
	release := make(chan struct{})
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			close(started)
			<-release // work that does not watch ctx, such as a committed transaction
			return nil
		}))
	stop := runConsumer(t, c)

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not handle the event")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	stop()

	n, err := pendingCount(context.Background(), client, stream, group)
	require.NoError(t, err)
	assert.Zero(t, n, "a handler that finished is acknowledged though shutdown began")
}

func TestConsumerReadsNoMoreThanItsFreeSlots(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)
	for i := 0; i < 3; i++ {
		publishEnvelope(t, client, stream, testEnvelope(t, "wid_"+strconv.Itoa(i)))
	}

	started := make(chan struct{}, 3)
	release := make(chan struct{})
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(context.Context, events.Delivery) error {
			started <- struct{}{}
			<-release
			return nil
		}), func(c *ConsumerConfig) {
			c.Batch = 10
			c.Concurrency = 1
			c.ClaimIdleTimeout = 5 * time.Second
		})
	stop := runConsumer(t, c)

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not handle the event")
	}
	time.Sleep(100 * time.Millisecond)
	requirePending(t, client, stream, group, 1)

	close(release)
	requirePending(t, client, stream, group, 0)
	assert.Len(t, started, 2, "the other entries are read once a slot frees")
	stop()
}

func TestConsumerKeepsRunningAfterARedisError(t *testing.T) {
	client := redisClient(t)
	stream, group, dead := testStreamNames(t, client)

	received := make(chan string, 1)
	c := newTestConsumer(t, client, stream, group, "replica-1", dead,
		events.DeliveryHandlerFunc(func(_ context.Context, d events.Delivery) error {
			received <- d.Envelope().ID
			return nil
		}))
	stop := runConsumer(t, c)

	// Reads fail with NOGROUP until the group is back.
	ctx := context.Background()
	require.Eventually(t, func() bool {
		n, err := client.XGroupDestroy(ctx, stream, group).Result()
		return err == nil && n == 1
	}, 3*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, client.XGroupCreate(ctx, stream, group, "0").Err())

	envelope := testEnvelope(t, "wid_1")
	publishEnvelope(t, client, stream, envelope)
	select {
	case id := <-received:
		assert.Equal(t, envelope.ID, id)
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not resume after the error")
	}
	stop()
}

func streamLen(t *testing.T, client *redis.Client, stream string) int64 {
	t.Helper()
	n, err := client.XLen(context.Background(), stream).Result()
	require.NoError(t, err)
	return n
}

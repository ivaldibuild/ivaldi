package redisstream

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

const (
	// DefaultBatch is the most entries one read or claim takes.
	DefaultBatch = 100
	// DefaultConcurrency is how many deliveries one consumer handles at once.
	DefaultConcurrency = 10
	// DefaultBlockTimeout is how long one read waits for new entries.
	DefaultBlockTimeout = 5 * time.Second
	// DefaultClaimIdleTimeout is how long a pending entry may stay idle
	// before another consumer claims it.
	DefaultClaimIdleTimeout = 5 * time.Minute
	// DefaultMaxDeliveries is how often an entry is delivered before it is
	// dead-lettered.
	DefaultMaxDeliveries = 5
)

// ConsumerConfig holds a Consumer's collaborators and tuning. Client, Stream,
// Group, Consumer, Handler, and DeadLetterStream are required; every other
// field has a default.
type ConsumerConfig struct {
	// Client is the Redis connection. The caller owns TLS, ACLs, and
	// persistence, so the consumer never sees credentials.
	Client redis.UniversalClient
	// Stream is the stream events are read from.
	Stream string
	// Group is the consumer group shared by every replica of one deployable.
	Group string
	// Consumer identifies this replica in Group and must be unique while it
	// runs. It also names the replica's pending entries, so keep it stable
	// across restarts where possible.
	Consumer string
	// Start is the ID a newly created group begins at: "0" replays the
	// retained stream, and "$" starts at only new entries. It is ignored when
	// the group already exists. Default "0", because the stream may hold
	// events published while no consumer was running.
	Start string
	// DisableGroupCreation fails instead of creating a missing group. Use it
	// where an operator creates and monitors the group itself.
	DisableGroupCreation bool

	// Handler receives each delivery.
	Handler events.DeliveryHandler
	// ManualAck means the consumer does not acknowledge a delivery itself.
	// The handler must call Delivery.Ack. With the default, the consumer
	// acknowledges after the handler returns nil. A Router does not call Ack,
	// so use auto-ack with one, or wrap it in a handler that acknowledges.
	ManualAck bool

	// Batch is the most entries one read or claim takes. Default DefaultBatch.
	Batch int
	// Concurrency is how many deliveries are handled at once. Default
	// DefaultConcurrency.
	Concurrency int
	// BlockTimeout is how long one read waits for new entries. Default
	// DefaultBlockTimeout.
	BlockTimeout time.Duration
	// ClaimIdleTimeout is how long a pending entry may stay idle before this
	// consumer claims it. It must be longer than the slowest handler.
	// Default DefaultClaimIdleTimeout.
	ClaimIdleTimeout time.Duration
	// MaxDeliveries is how many times an entry is delivered before it is
	// dead-lettered. Default DefaultMaxDeliveries.
	MaxDeliveries int
	// DeadLetterStream receives entries this consumer gives up on. It must
	// differ from Stream.
	DeadLetterStream string
	// DeadLetterMaxLen is the approximate maximum dead-letter stream length;
	// zero means no trimming, so operators do not lose events silently.
	DeadLetterMaxLen int64

	// Logger reports retries and dead letters. It may be nil, in which case
	// the consumer is silent.
	Logger logging.Logger
}

// Consumer reads entries from a Redis stream with a consumer group and hands
// each to an events.DeliveryHandler.
//
// A consumer group remembers its position in the stream and keeps every
// delivered but unacknowledged entry in its pending list. On startup the
// consumer drains its own pending entries; while running it claims entries
// left idle by a stopped consumer. Delivery is therefore at least once, and
// handlers must be idempotent.
type Consumer struct {
	client               redis.UniversalClient
	stream               string
	group                string
	consumer             string
	start                string
	disableGroupCreation bool
	handler              events.DeliveryHandler
	manualAck            bool
	batch                int
	concurrency          int
	blockTimeout         time.Duration
	claimIdle            time.Duration
	maxDeliveries        int
	deadLetterStream     string
	deadLetterMaxLen     int64
	logger               logging.Logger
}

var _ events.Consumer = (*Consumer)(nil)

// NewConsumer returns a Consumer, or an error naming a missing collaborator
// or an invalid setting.
func NewConsumer(c ConsumerConfig) (*Consumer, error) {
	if c.Client == nil {
		return nil, errors.New("redisstream: Client is required")
	}
	if c.Stream == "" {
		return nil, errors.New("redisstream: Stream is required")
	}
	if c.Group == "" {
		return nil, errors.New("redisstream: Group is required")
	}
	if c.Consumer == "" {
		return nil, errors.New("redisstream: Consumer is required")
	}
	if c.Handler == nil {
		return nil, errors.New("redisstream: Handler is required")
	}
	if c.DeadLetterStream == "" {
		return nil, errors.New("redisstream: DeadLetterStream is required")
	}
	if c.DeadLetterStream == c.Stream {
		return nil, errors.New("redisstream: DeadLetterStream must differ from Stream")
	}

	start := c.Start
	if start == "" {
		start = "0"
	}
	batch := c.Batch
	if batch == 0 {
		batch = DefaultBatch
	}
	concurrency := c.Concurrency
	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}
	blockTimeout := c.BlockTimeout
	if blockTimeout == 0 {
		blockTimeout = DefaultBlockTimeout
	}
	claimIdle := c.ClaimIdleTimeout
	if claimIdle == 0 {
		claimIdle = DefaultClaimIdleTimeout
	}
	maxDeliveries := c.MaxDeliveries
	if maxDeliveries == 0 {
		maxDeliveries = DefaultMaxDeliveries
	}

	invalid := []struct {
		name string
		bad  bool
	}{
		{"Batch", batch < 0},
		{"Concurrency", concurrency < 0},
		{"BlockTimeout", blockTimeout < 0},
		{"ClaimIdleTimeout", claimIdle < 0},
		{"MaxDeliveries", maxDeliveries < 0},
		{"DeadLetterMaxLen", c.DeadLetterMaxLen < 0},
	}
	for _, v := range invalid {
		if v.bad {
			return nil, fmt.Errorf("redisstream: %s is invalid", v.name)
		}
	}
	return &Consumer{
		client:               c.Client,
		stream:               c.Stream,
		group:                c.Group,
		consumer:             c.Consumer,
		start:                start,
		disableGroupCreation: c.DisableGroupCreation,
		handler:              c.Handler,
		manualAck:            c.ManualAck,
		batch:                batch,
		concurrency:          concurrency,
		blockTimeout:         blockTimeout,
		claimIdle:            claimIdle,
		maxDeliveries:        maxDeliveries,
		deadLetterStream:     c.DeadLetterStream,
		deadLetterMaxLen:     c.DeadLetterMaxLen,
		logger:               c.Logger,
	}, nil
}

// Run consumes entries until ctx is done. It first drains this consumer's
// pending entries left by an earlier run, then reads new entries while
// claiming entries other consumers left idle. It stops reading on ctx
// cancellation and waits for in-flight handlers. It never deletes the group
// or this consumer's pending list, which is how a restart recovers them.
func (c *Consumer) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	r := &runner{c: c, sem: make(chan struct{}, c.concurrency)}
	defer func() {
		cancel()
		r.wg.Wait()
	}()

	if err := c.ensureGroup(runCtx); err != nil {
		return err
	}
	if err := r.drainPending(runCtx); err != nil {
		if runCtx.Err() != nil {
			return nil
		}
		return err
	}

	for {
		if runCtx.Err() != nil {
			return nil
		}
		if err := r.reclaim(runCtx); err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			return err
		}
		entries, err := c.read(runCtx)
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("redisstream: read %s: %w", c.stream, err)
		}
		r.dispatch(runCtx, entries, nil)
	}
}

// Progress reports where the consumer group is in the stream. It is
// observational: the pending list and the group's last delivered ID, not a
// separate checkpoint, are what recovery uses.
type Progress struct {
	LastDeliveredID string
	Pending         int64
	Consumers       int64
	Lag             int64
}

// Progress reads the consumer group's position from Redis.
func (c *Consumer) Progress(ctx context.Context) (Progress, error) {
	groups, err := c.client.XInfoGroups(ctx, c.stream).Result()
	if err != nil {
		return Progress{}, fmt.Errorf("redisstream: inspect groups of %s: %w", c.stream, err)
	}
	for _, g := range groups {
		if g.Name == c.group {
			return Progress{
				LastDeliveredID: g.LastDeliveredID,
				Pending:         g.Pending,
				Consumers:       g.Consumers,
				Lag:             g.Lag,
			}, nil
		}
	}
	return Progress{}, fmt.Errorf("redisstream: group %q does not exist on %s", c.group, c.stream)
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	if c.disableGroupCreation {
		groups, err := c.client.XInfoGroups(ctx, c.stream).Result()
		if err != nil {
			return fmt.Errorf("redisstream: inspect groups of %s: %w", c.stream, err)
		}
		for _, g := range groups {
			if g.Name == c.group {
				return nil
			}
		}
		return fmt.Errorf("redisstream: group %q does not exist on %s", c.group, c.stream)
	}
	err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, c.start).Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("redisstream: create group %q on %s: %w", c.group, c.stream, err)
	}
	return nil
}

func (c *Consumer) read(ctx context.Context) ([]redis.XMessage, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{c.stream, ">"},
		Count:    int64(c.batch),
		Block:    c.blockTimeout,
	}).Result()
	if err != nil {
		return nil, err
	}
	for _, s := range streams {
		if s.Stream == c.stream {
			return s.Messages, nil
		}
	}
	return nil, nil
}

func (c *Consumer) claim(ctx context.Context, ids []string, minIdle time.Duration) ([]redis.XMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	entries, err := c.client.XClaim(ctx, &redis.XClaimArgs{
		Stream:   c.stream,
		Group:    c.group,
		Consumer: c.consumer,
		MinIdle:  minIdle,
		Messages: ids,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("redisstream: claim %d entries from %s: %w", len(ids), c.stream, err)
	}
	return entries, nil
}

func (c *Consumer) acknowledge(ctx context.Context, entryID string) error {
	if err := c.client.XAck(ctx, c.stream, c.group, entryID).Err(); err != nil {
		return fmt.Errorf("acknowledge entry %s: %w", entryID, err)
	}
	return nil
}

func (c *Consumer) deadLetter(ctx context.Context, entry redis.XMessage, deliveries int64, cause error) {
	values := deadLetterValues(entry.Values, entry.ID, c.stream, c.group, c.consumer, deliveries, cause)
	args := &redis.XAddArgs{Stream: c.deadLetterStream, Values: values}
	if c.deadLetterMaxLen > 0 {
		args.MaxLen = c.deadLetterMaxLen
		args.Approx = true
	}
	// Add before acknowledging: a failed write must leave the entry pending,
	// even if that means a duplicate on the next delivery. Acknowledge only
	// once the dead letter exists.
	if err := c.client.XAdd(ctx, args).Err(); err != nil {
		c.logError("redisstream: dead-letter event", entry, deliveries, cause,
			logging.F("dead_letter_error", err))
		return
	}
	if err := c.client.XAck(ctx, c.stream, c.group, entry.ID).Err(); err != nil {
		c.logError("redisstream: acknowledge dead-lettered event", entry, deliveries, cause,
			logging.F("dead_letter_error", err))
		return
	}
	c.logInfo("redisstream: event dead-lettered", entry, deliveries,
		logging.F("dead_letter_stream", c.deadLetterStream))
}

func (c *Consumer) logInfo(msg string, entry redis.XMessage, deliveries int64, extra ...logging.Field) {
	if c.logger == nil {
		return
	}
	c.logger.Info(msg, c.logFields(entry, deliveries, extra...)...)
}

func (c *Consumer) logError(msg string, entry redis.XMessage, deliveries int64, err error, extra ...logging.Field) {
	if c.logger == nil {
		return
	}
	fields := append(c.logFields(entry, deliveries, extra...), logging.F("error", err))
	c.logger.Error(msg, fields...)
}

func (c *Consumer) logFields(entry redis.XMessage, deliveries int64, extra ...logging.Field) []logging.Field {
	eventID, _ := stringField(entry.Values, "id")
	eventType, _ := stringField(entry.Values, "type")
	fields := []logging.Field{
		logging.F("stream", c.stream),
		logging.F("group", c.group),
		logging.F("consumer", c.consumer),
		logging.F("entry_id", entry.ID),
		logging.F("event_id", eventID),
		logging.F("event_type", eventType),
		logging.F("deliveries", deliveries),
	}
	return append(fields, extra...)
}

// runner holds the state one Run needs. Keeping it out of Consumer lets a
// Consumer be configured once and run without carrying goroutine state.
type runner struct {
	c   *Consumer
	sem chan struct{}
	wg  sync.WaitGroup
}

// drainPending claims this consumer's own pending entries from earlier runs.
// It walks the pending list with an exclusive cursor so entries already
// claimed in this run are not claimed again before their handlers finish.
func (r *runner) drainPending(ctx context.Context) error {
	start := "-"
	for {
		if ctx.Err() != nil {
			return nil
		}
		pending, err := r.c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream:   r.c.stream,
			Group:    r.c.group,
			Start:    start,
			End:      "+",
			Count:    int64(r.c.batch),
			Consumer: r.c.consumer,
		}).Result()
		if err != nil {
			return fmt.Errorf("redisstream: inspect pending entries of %s: %w", r.c.consumer, err)
		}
		if len(pending) == 0 {
			return nil
		}

		ids := make([]string, len(pending))
		deliveries := make(map[string]int64, len(pending))
		for i, p := range pending {
			ids[i] = p.ID
			deliveries[p.ID] = p.RetryCount + 1
		}
		entries, err := r.c.claim(ctx, ids, 0)
		if err != nil {
			return err
		}
		r.dispatch(ctx, entries, deliveries)

		next, err := nextStreamID(pending[len(pending)-1].ID)
		if err != nil {
			return err
		}
		start = next
	}
}

// reclaim claims pending entries left idle by any consumer. XCLAIM resets
// their idle time, so repeated calls drain the idle set rather than returning
// the same entries.
func (r *runner) reclaim(ctx context.Context) error {
	seen := make(map[string]struct{})
	for {
		if ctx.Err() != nil {
			return nil
		}
		pending, err := r.c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: r.c.stream,
			Group:  r.c.group,
			Idle:   r.c.claimIdle,
			Start:  "-",
			End:    "+",
			Count:  int64(r.c.batch),
		}).Result()
		if err != nil {
			return fmt.Errorf("redisstream: inspect idle pending entries of %s: %w", r.c.stream, err)
		}
		if len(pending) == 0 {
			return nil
		}

		ids := make([]string, 0, len(pending))
		deliveries := make(map[string]int64, len(pending))
		for _, p := range pending {
			if _, ok := seen[p.ID]; ok {
				continue
			}
			seen[p.ID] = struct{}{}
			ids = append(ids, p.ID)
			deliveries[p.ID] = p.RetryCount + 1
		}
		if len(ids) == 0 {
			return nil
		}
		entries, err := r.c.claim(ctx, ids, r.c.claimIdle)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		r.dispatch(ctx, entries, deliveries)
	}
}

func (r *runner) dispatch(ctx context.Context, entries []redis.XMessage, deliveries map[string]int64) {
	for _, entry := range entries {
		n := int64(1)
		if deliveries != nil {
			if attempt, ok := deliveries[entry.ID]; ok {
				n = attempt
			}
		}
		select {
		case r.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		r.wg.Add(1)
		go func(entry redis.XMessage, deliveries int64) {
			defer r.wg.Done()
			defer func() { <-r.sem }()
			r.handle(ctx, entry, deliveries)
		}(entry, n)
	}
}

func (r *runner) handle(ctx context.Context, entry redis.XMessage, deliveries int64) {
	c := r.c
	envelope, err := envelopeFromFields(entry.Values)
	if err != nil {
		c.deadLetter(ctx, entry, deliveries, fmt.Errorf("decode entry %s: %w", entry.ID, err))
		return
	}

	d := &delivery{
		envelope:   envelope,
		entryID:    entry.ID,
		deliveries: deliveries,
		ack: func(ctx context.Context) error {
			return c.acknowledge(ctx, entry.ID)
		},
	}
	handleErr := c.handler.HandleDelivery(ctx, d)
	if handleErr == nil {
		if c.manualAck {
			if !d.isAcked() {
				c.logInfo("redisstream: delivery returned without acknowledgement", entry, deliveries)
			}
			return
		}
		if ackErr := d.Ack(ctx); ackErr != nil {
			c.logError("redisstream: acknowledge event", entry, deliveries, ackErr)
		}
		return
	}

	if d.isAcked() {
		c.logError("redisstream: handler failed after acknowledgement", entry, deliveries, handleErr)
		return
	}
	if ctx.Err() != nil {
		// Shutdown interrupted the handler. Leave the entry pending for the
		// next run rather than dead-lettering it for a cancellation.
		return
	}
	if deliveries >= int64(c.maxDeliveries) {
		c.deadLetter(ctx, entry, deliveries, handleErr)
		return
	}
	c.logError("redisstream: event will be retried", entry, deliveries, handleErr)
}

// delivery is the transport's events.Delivery. It acknowledges through the
// consumer's XACK and is idempotent.
type delivery struct {
	envelope   events.Envelope
	entryID    string
	deliveries int64
	ack        func(context.Context) error

	mu    sync.Mutex
	acked bool
}

func (d *delivery) Envelope() events.Envelope { return d.envelope }

func (d *delivery) EntryID() string { return d.entryID }

func (d *delivery) DeliveryCount() int64 { return d.deliveries }

func (d *delivery) Ack(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.acked {
		return nil
	}
	if err := d.ack(ctx); err != nil {
		return err
	}
	d.acked = true
	return nil
}

func (d *delivery) isAcked() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.acked
}

// deadLetterValues copies an entry's contract fields and adds what an
// operator needs to triage it.
func deadLetterValues(values map[string]any, entryID, sourceStream, group, consumer string, deliveries int64, cause error) map[string]any {
	out := make(map[string]any, len(values)+7)
	for k, v := range values {
		out[k] = v
	}
	out["error"] = cause.Error()
	out["failed_at"] = clock.Now().UTC().Format(time.RFC3339Nano)
	out["deliveries"] = strconv.FormatInt(deliveries, 10)
	out["source_stream"] = sourceStream
	out["source_entry_id"] = entryID
	out["consumer_group"] = group
	out["consumer"] = consumer
	return out
}

// nextStreamID returns the smallest stream ID greater than id. XPENDING has
// no exclusive-range syntax in every supported Redis version, so paging walks
// past a page's last ID by incrementing it.
func nextStreamID(id string) (string, error) {
	millisPart, seqPart, ok := strings.Cut(id, "-")
	if !ok {
		return "", fmt.Errorf("redisstream: invalid stream ID %q", id)
	}
	millis, err := strconv.ParseUint(millisPart, 10, 64)
	if err != nil {
		return "", fmt.Errorf("redisstream: invalid stream ID %q: %w", id, err)
	}
	seq, err := strconv.ParseUint(seqPart, 10, 64)
	if err != nil {
		return "", fmt.Errorf("redisstream: invalid stream ID %q: %w", id, err)
	}
	if seq == ^uint64(0) {
		if millis == ^uint64(0) {
			return "", fmt.Errorf("redisstream: stream ID %q has no successor", id)
		}
		millis++
		seq = 0
	} else {
		seq++
	}
	return fmt.Sprintf("%d-%d", millis, seq), nil
}

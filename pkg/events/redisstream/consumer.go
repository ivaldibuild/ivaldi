package redisstream

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
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
	// DefaultRetryInterval is how long Run waits after a Redis error before
	// trying again.
	DefaultRetryInterval = time.Second

	// settleTimeout bounds one acknowledgement or dead letter. Both run even
	// after shutdown has cancelled the handler's context, so a handler that
	// finished is not redelivered for want of an XACK.
	settleTimeout = 5 * time.Second
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
	// The handler must call Delivery.Ack within ClaimIdleTimeout. With the
	// default, the consumer acknowledges after the handler returns nil. A
	// Router does not call Ack, so use auto-ack with one, or wrap it in a
	// handler that acknowledges. A delivery left unacknowledged is redelivered
	// and, after MaxDeliveries, dead-lettered.
	ManualAck bool

	// Batch is the most entries one read or claim takes. A read or claim never
	// takes more entries than there are free handler slots, so an entry starts
	// as soon as it is read. Default DefaultBatch.
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
	// dead-lettered. A handler error matching events.ErrPermanent
	// dead-letters the entry at its first delivery. An entry whose earlier deliveries never finished, because
	// the process stopped or a manual-ack handler never acknowledged, is
	// dead-lettered without reaching the handler once it is delivered more
	// often than this. Default DefaultMaxDeliveries.
	MaxDeliveries int
	// RetryInterval is how long Run waits after a Redis error before trying
	// again. Default DefaultRetryInterval.
	RetryInterval time.Duration
	// DeadLetterStream receives entries this consumer gives up on. It must
	// differ from Stream.
	DeadLetterStream string
	// DeadLetterMaxLen is the approximate maximum dead-letter stream length;
	// zero means no trimming, so operators do not lose events silently.
	DeadLetterMaxLen int64

	// Logger reports Redis errors, retries, panics, and dead letters. It may
	// be nil, in which case the consumer is silent.
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
	retryInterval        time.Duration
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
	retryInterval := c.RetryInterval
	if retryInterval == 0 {
		retryInterval = DefaultRetryInterval
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
		{"RetryInterval", retryInterval < 0},
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
		retryInterval:        retryInterval,
		deadLetterStream:     c.DeadLetterStream,
		deadLetterMaxLen:     c.DeadLetterMaxLen,
		logger:               c.Logger,
	}, nil
}

// Run consumes entries until ctx is done. It first drains this consumer's
// pending entries left by an earlier run, then reads new entries while
// claiming entries other consumers left idle. It returns an error only when
// the group cannot be created or found at startup; after that it logs Redis
// errors and tries again after RetryInterval. It stops reading on ctx
// cancellation and waits for in-flight handlers, whose context is cancelled
// too. It never deletes the group or this consumer's pending list, which is
// how a restart recovers them.
func (c *Consumer) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	r := &runner{c: c, sem: make(chan struct{}, c.concurrency), drainFrom: "-"}
	defer func() {
		cancel()
		r.wg.Wait()
	}()

	if err := c.ensureGroup(runCtx); err != nil {
		return err
	}
	for runCtx.Err() == nil {
		err := r.step(runCtx)
		if err == nil || runCtx.Err() != nil {
			continue
		}
		if c.logger != nil {
			c.logger.Error("redisstream: consumer will retry after a Redis error",
				logging.F("stream", c.stream),
				logging.F("group", c.group),
				logging.F("consumer", c.consumer),
				logging.F("retry_in", c.retryInterval.String()),
				logging.F("error", err.Error()))
		}
		select {
		case <-runCtx.Done():
		case <-time.After(c.retryInterval):
		}
	}
	return nil
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

func (c *Consumer) read(ctx context.Context, count int) ([]redis.XMessage, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{c.stream, ">"},
		Count:    int64(count),
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

// settleContext keeps ctx's values but not its cancellation, so the outcome
// of a handler that finished is recorded even when shutdown has begun.
func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

func (c *Consumer) acknowledge(ctx context.Context, entryID string) error {
	ctx, cancel := settleContext(ctx)
	defer cancel()
	if err := c.client.XAck(ctx, c.stream, c.group, entryID).Err(); err != nil {
		return fmt.Errorf("acknowledge entry %s: %w", entryID, err)
	}
	return nil
}

func (c *Consumer) deadLetter(ctx context.Context, entry redis.XMessage, deliveries int64, cause error) {
	ctx, cancel := settleContext(ctx)
	defer cancel()
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
	fields := append(c.logFields(entry, deliveries, extra...), logging.F("error", err.Error()))
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

	// drainFrom is where drainPending resumes in this consumer's pending
	// list, kept across retries so an entry already claimed in this run is
	// not claimed again while its handler runs. drained is set once the
	// list has been walked.
	drainFrom string
	drained   bool
	// lastReclaim is when reclaim last looked for idle entries.
	lastReclaim time.Time
}

// step does one unit of work: it drains this consumer's pending entries until
// none are left, and after that reclaims idle entries and reads new ones.
func (r *runner) step(ctx context.Context) error {
	if !r.drained {
		return r.drainPending(ctx)
	}
	if err := r.reclaim(ctx); err != nil {
		return err
	}
	count, ok := r.free(ctx)
	if !ok {
		return nil
	}
	entries, err := r.c.read(ctx, count)
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("redisstream: read %s: %w", r.c.stream, err)
	}
	r.dispatch(ctx, entries, nil)
	return nil
}

// free waits until a handler slot is free and returns how many are, at most
// the batch size. Reading or claiming no more than that keeps an entry from
// sitting in the pending list, growing idle, while it waits for a slot,
// which would let another consumer claim it before it starts. Only the Run
// goroutine takes slots, so the count can only grow before dispatch.
func (r *runner) free(ctx context.Context) (int, bool) {
	select {
	case r.sem <- struct{}{}:
		<-r.sem
	case <-ctx.Done():
		return 0, false
	}
	return min(cap(r.sem)-len(r.sem), r.c.batch), true
}

// drainPending claims this consumer's own pending entries from earlier runs.
// It walks the pending list with an exclusive cursor so entries already
// claimed in this run are not claimed again before their handlers finish.
func (r *runner) drainPending(ctx context.Context) error {
	for {
		count, ok := r.free(ctx)
		if !ok {
			return nil
		}
		pending, err := r.c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream:   r.c.stream,
			Group:    r.c.group,
			Start:    r.drainFrom,
			End:      "+",
			Count:    int64(count),
			Consumer: r.c.consumer,
		}).Result()
		if err != nil {
			return fmt.Errorf("redisstream: inspect pending entries of %s: %w", r.c.consumer, err)
		}
		if len(pending) == 0 {
			r.drained = true
			return nil
		}

		ids := make([]string, len(pending))
		deliveries := make(map[string]int64, len(pending))
		for i, p := range pending {
			ids[i] = p.ID
			deliveries[p.ID] = p.RetryCount + 1
		}
		next, err := nextStreamID(pending[len(pending)-1].ID)
		if err != nil {
			return err
		}
		entries, err := r.c.claim(ctx, ids, 0)
		if err != nil {
			return err
		}
		r.drainFrom = next
		r.dispatch(ctx, entries, deliveries)
	}
}

// reclaim claims pending entries left idle by any consumer, at most every
// half ClaimIdleTimeout: an entry becomes claimable only after that long, so
// looking more often finds little and costs a pending-list scan per read.
// XCLAIM resets the idle time of what it claims, so repeated passes drain the
// idle set rather than returning the same entries.
func (r *runner) reclaim(ctx context.Context) error {
	if !r.lastReclaim.IsZero() && time.Since(r.lastReclaim) < r.c.claimIdle/2 {
		return nil
	}
	r.lastReclaim = time.Now()
	seen := make(map[string]struct{})
	for {
		count, ok := r.free(ctx)
		if !ok {
			return nil
		}
		pending, err := r.c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: r.c.stream,
			Group:  r.c.group,
			Idle:   r.c.claimIdle,
			Start:  "-",
			End:    "+",
			Count:  int64(count),
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
	if deliveries > int64(c.maxDeliveries) {
		// Every earlier delivery ended without an outcome: the process
		// stopped, perhaps because the handler crashed it, or a manual-ack
		// handler never acknowledged. Give up rather than hand the entry
		// back to a handler that may stop the process again.
		c.deadLetter(ctx, entry, deliveries,
			fmt.Errorf("no outcome after %d deliveries", deliveries-1))
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
	handleErr := r.call(ctx, entry, d)
	if handleErr == nil {
		if c.manualAck {
			if !d.isAcked() {
				c.logInfo("redisstream: delivery returned without acknowledgement; it will be redelivered", entry, deliveries)
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
	if deliveries >= int64(c.maxDeliveries) || errors.Is(handleErr, events.ErrPermanent) {
		c.deadLetter(ctx, entry, deliveries, handleErr)
		return
	}
	c.logError("redisstream: event will be retried", entry, deliveries, handleErr)
}

// call runs the handler and turns a panic into an error, so one bad event
// cannot stop the process; the entry is then retried and dead-lettered like
// any other failure. The stack goes to the log, not the dead letter.
func (r *runner) call(ctx context.Context, entry redis.XMessage, d *delivery) (err error) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		err = fmt.Errorf("handler panicked: %v", p)
		if r.c.logger != nil {
			r.c.logger.Error("redisstream: handler panicked",
				r.c.logFields(entry, d.deliveries,
					logging.F("panic", fmt.Sprint(p)),
					logging.F("stack", string(debug.Stack())))...)
		}
	}()
	return r.c.handler.HandleDelivery(ctx, d)
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

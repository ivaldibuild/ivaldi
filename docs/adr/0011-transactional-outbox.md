# ADR 0011: A transactional outbox for control-plane domain events

- Status: Proposed
- Date: 2026-11-02
- Related: [ADR 0002](0002-gateway-integration-ingress.md), [ADR 0005](0005-data-model-ownership.md), [ADR 0009](0009-gateway-event-transport.md), [ADR 0010](0010-one-binary-for-the-components.md)

## Context

Capabilities in the control plane already record domain events. A domain
object's methods call `RecordEvent`, and `pkg/events.Wrap` turns what it
recorded into an `Envelope` with an ID, type, Organization, aggregate,
correlation chain, and JSON payload. Until now, nothing persisted those
envelopes: every service change dropped the events it raised.

The events must be durable and must not outlive a rolled-back change. A
service that saves an object and then publishes to a transport separately can
fail between the two writes: either the change commits with no event, or an
event describes a change that did not commit. The fix is the transactional
outbox pattern: write the event in the transaction that writes the object,
and let a separate relay dispatch it afterwards.

[ADR 0009](0009-gateway-event-transport.md) solves the same problem for events
arriving *from* the gateway, using Redis Streams and a consumer group. That
stream stays outside the control-plane database because the gateway must not
write to it. It says internal asynchronous work "needs its own decision,
though it may well use the same mechanism." This is that decision.

## Decision

### The outbox is a control-plane table, not a shared package

The outbox owns the `outbox` schema in the control-plane database, with its
migrations in `apps/control-plane/internal/outbox/migrations` (ADR 0005). Its
Go package is `apps/control-plane/internal/outbox`, beside `audit`, because
only the control plane may reach the database. The package is not in `pkg/`:
`pkg/` holds code more than one deployable needs, and a database row is not
that.

The transport contract stays in `pkg/events`. The outbox depends on
`events.Envelope`, `events.Handler`, and `events.Publisher`; it never imports a
transport driver.

### Services record events in the transaction that changes the object

`outbox.Recorder.Record` inserts envelopes through the transaction carried by
the context (`pkg/database`), so the event and the object commit or roll back
together. A capability pulls the events its object recorded only after saving
it, and writes them before the transaction ends. The record is idempotent on
the event ID, so a retried write adds nothing.

Delivery is wired this way first: every create and every change that goes
through its `run` helper calls `recordEvents`. Other capabilities follow as
their services gain writes; IAM has domain objects but no service yet.

### A relay claims events with leases and dispatches them at least once

`outbox.Relay` claims a batch with `SELECT … FOR UPDATE SKIP LOCKED`, so
replicas claim different entries, and leases them for a configurable time. It
dispatches entries oldest first, and after the batch marks published together
the entries whose handlers returned nil. A handler failure increments the
attempt count and schedules a retry with exponential backoff; after the
maximum number of attempts the entry is dead-lettered instead of retried. A
handler that fails because its transport is down, not because of the event,
returns `events.ErrUnavailable`: the relay then stops the batch and leaves the
undelivered entries leased, counting no attempt, so an outage of the stream
delays events but never dead-letters them. An entry whose lease expires is
claimed again, which is why handling is at least once. Every mark names the
claim it comes from, and a mark from a claim that a later one replaced changes
nothing, so a relay that outlived its lease cannot release, retry, or publish
an entry another relay now holds.

Events that are published but not consumed are still in the stream; events
that are published and no longer needed are deleted by the relay after a
retention window. Dead-lettered entries stay in the table for inspection.

Ordering is best effort. `pkg/events` already tells handlers that delivery may
be out of order, and the relay does not serialize by aggregate. A consumer
that needs per-aggregate order must add a partition key and one consumer per
partition.

### The relay publishes to a dedicated Redis stream

`pkg/events/redisstream` implements `events.Publisher` over Redis Streams
using the same mechanism as ADR 0009, but a different stream from the
gateway's. It writes one flat entry per envelope (see
`schemas/event-envelope.v1.json`) with approximate `MAXLEN` trimming, and the
caller supplies the client so TLS, ACLs, and persistence remain the operator's
choice. Each consuming deployable reads the stream with its own consumer
group; the consumer is not part of this decision.

The relay is transport-neutral: it dispatches to an `events.Handler`. An
in-process `events.Router` needs no Redis, and `events.Forward(publisher)`
adapts the Redis publisher. This keeps a deployment without Redis possible for
events that only local handlers consume.

### The relay runs in the control plane

The relay reads the control-plane database, so it belongs to `serve`, not to
the `worker`, `gateway`, or `indexer` components. ADR 0010's boundary test
forbids a component from importing the control plane's internal packages, and
that rule is correct here: a data-plane component must not open the
control-plane database. A future data-plane consumer gets events over the
stream or the runner connection, not by reading this table.

## Consequences

- **Every change writes one more row.** The write is in the same transaction,
  so it adds no failure mode that the change did not already have, but it does
  add a row and an index entry per event.
- **Delivery is at least once, never exactly once.** A crash after a handler
  commits but before its `MarkPublished` redelivers the event. Handlers must be
  idempotent, as `pkg/events` already requires.
- **A slow handler can outlive its lease** and run twice at once. The lease
  covers a whole claimed batch, not one event, because the relay dispatches
  the batch in order. The relay whose lease lapsed cannot overwrite the later
  claim's outcome, but its handler has already run. A handler or batch that needs longer than the lease must
  be split, the lease raised, or the batch lowered.
- **The table and the stream need retention policies.** Without the relay
  running, the table grows; without a consumer, the stream only holds entries
  until trimming. Both need monitoring, and dead-lettered rows need an
  operator path back.
- **Redis remains optional for local handlers.** A deployment that only runs
  in-process sagas can wire the relay to a Router and skip the publisher.
- **The gateway's stream is unchanged.** ADR 0009 still owns gateway ingress;
  this ADR adds a second stream for events raised inside the control plane.

## Alternatives considered

- **Publish inside the change's transaction.** Rejected: the transport would
  join a database transaction, and a publish could not be rolled back with it.
- **Publish after the transaction commits.** Rejected: a crash between commit
  and publish loses the event. This is the dual-write problem the outbox
  exists to remove.
- **PostgreSQL `LISTEN`/`NOTIFY` instead of a relay.** Rejected for now: it
  wakes listeners but keeps no durable record, no retry state, and no
  dead-letter path.
- **A queue table for gateway events.** Already rejected in ADR 0009: the
  gateway must not write to the control-plane database.
- **Redis for every internal event, with no in-process path.** Rejected:
  deployments that only need local handlers should not require Redis, and the
  relay's `events.Handler` seam costs nothing.

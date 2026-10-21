# ADR 0009: Redis Streams carry events from the gateway to the control plane

- Status: Proposed
- Date: 2026-10-20

## Context

The gateway receives provider webhooks, verifies them, normalizes them into Ivaldi events, and hands them off without calling domain logic ([ADR 0002](0002-gateway-integration-ingress.md)). ADR 0002 says the gateway enqueues events but does not choose how. `AGENTS.md` listed AWS SQS as the current direction.

Whatever carries these events has to:

- **work the same way in every deployment mode** ([ADR 0001](0001-deployment-topology.md)). Self-hosted installations must not need AWS, and the modes must not take different code paths;
- **let the gateway acknowledge the provider quickly**, and keep events while the control plane is down or busy;
- **deliver each event at least once** to one of several control-plane replicas, and retry events whose handling failed;
- **keep events the control plane gave up on** where an operator can see them, rather than dropping them;
- **stay outside the control-plane database.** The control plane owns its database ([ADR 0005](0005-data-model-ownership.md)), so the gateway should not write to it, even as a queue.

## Decision

**The gateway publishes events to a Redis stream, and the control plane consumes them through a consumer group.**

- **One stream for gateway events.** Each entry carries the event's type, contract version, Organization, an event ID, and its payload. The payload is a versioned contract in `schemas/` ([ADR 0005](0005-data-model-ownership.md)), because the gateway and the control plane can run different versions.
- **Each consuming deployable has a consumer group, and each replica is a consumer in it.** The control plane's group is `control-plane`, so every event is handled by one replica. Another deployable that needs the same events gets its own group, without the gateway changing.
- **An event is acknowledged after its handling commits.** The control plane reads with `XREADGROUP`, handles the event in a transaction, and calls `XACK` only after that commits. Delivery is therefore at least once.
- **Handlers are idempotent.** The gateway derives the event ID from the provider's delivery ID, which it already uses against replays. The control plane records the IDs it has handled, so a redelivered event changes nothing.
- **Stalled events are claimed by another replica.** An entry left pending longer than the claim timeout, for example because its replica crashed, is taken over with `XAUTOCLAIM`.
- **Events that keep failing are moved aside.** After the maximum number of deliveries, an entry is copied to a dead-letter stream with its last error and acknowledged on the main stream. Operators can inspect and replay it from there.
- **The gateway trims the stream.** It caps the stream's length when it adds entries (`XADD … MAXLEN ~`). The cap must be far above any expected backlog, and consumer-group lag is monitored, because trimming can drop entries that have not been consumed.
- **Each side gets its own Redis user.** With Redis ACLs, the gateway's user may only add to the stream, and the control plane's user may read, acknowledge, claim, and write to the dead-letter stream. Both connect over TLS where the network is shared.
- **Redis sits behind an Ivaldi-owned interface.** `pkg/events` defines the publisher and consumer, and Redis Streams is their implementation. Domain code sees events, not Redis.

### Where it applies

Redis Streams carries events from a gateway running in the control plane's network: the managed cloud, or a self-hosted installation.

Under bring-your-own-compute, a gateway in the customer's data plane does not connect to the control plane's Redis, because the data plane reaches the control plane only through its outbound runner connection ([ADR 0001](0001-deployment-topology.md)). It relays events over that connection, as ADR 0002 describes. The control plane publishes relayed events to the same stream, so consumers handle every event the same way, wherever the gateway ran.

It carries only gateway events. Work assignments and lookups to runners go over the runner connection ([ADR 0008](0008-reaching-customer-systems.md)). Other asynchronous work inside the control plane needs its own decision, though it may well use the same mechanism.

## Consequences

- **Self-hosted installations need Redis as well as PostgreSQL.** Both are commonly available, including as managed services.
- **Redis must persist the stream.** It must run with append-only-file persistence and, in production, with replicas. With `appendfsync everysec`, a crash can lose up to about a second of events that the gateway already acknowledged to the provider. Providers' delivery logs and periodic reconciliation, such as the polling fallback in ADR 0002, recover what is lost.
- **Delivery is at least once, not exactly once.** Every handler must tolerate duplicates.
- **Ordering holds only within the stream as published.** Retries and dead-lettering mean handlers must not assume that events arrive in provider order.
- **Lag, pending entries, and the dead-letter stream need monitoring and alerts.** Trimming a stream that is behind loses events.
- **Event contracts are versioned**, so the gateway and the control plane can be upgraded separately.

## Alternatives considered

- **AWS SQS.** Rejected: self-hosted installations would need a second implementation or an AWS dependency, and the modes would drift. SQS also offers no replay, and needs a separate queue for each additional consumer.
- **A queue table in the control plane's PostgreSQL.** Rejected: the gateway would need to write to the control plane's database, which ADR 0005 keeps private to the control plane. Polling or `LISTEN` would also add load to the primary database on the webhook path.
- **NATS JetStream or Kafka.** Considered. Both are stronger at durable streaming and replay, but they are heavier to run, especially for self-hosted installations. Redis covers the current volume and is easier to operate. The `pkg/events` interface keeps a later switch open.
- **The gateway calling the control-plane API directly.** Rejected: the gateway could not acknowledge providers while the control plane was down or slow, and nothing would buffer bursts of events.

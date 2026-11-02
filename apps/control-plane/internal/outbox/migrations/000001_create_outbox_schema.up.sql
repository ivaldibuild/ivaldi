-- The outbox holds the domain events services raise until a relay dispatches
-- them. A service writes an event in the transaction that changes the object,
-- so an event exists only if its change committed.
CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
  id VARCHAR(30) PRIMARY KEY,
  type VARCHAR(255) NOT NULL,
  organization_id VARCHAR(30) NOT NULL,
  aggregate_type VARCHAR(255) NOT NULL,
  aggregate_id VARCHAR(30) NOT NULL,
  correlation_id VARCHAR(30) NOT NULL,
  causation_id VARCHAR(30) NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  payload JSONB NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NULL,
  claimed_until TIMESTAMPTZ NULL,
  dead_lettered_at TIMESTAMPTZ NULL,
  last_error TEXT NULL,
  -- An event is either published or dead-lettered, never both.
  CONSTRAINT chk_outbox_events_terminal CHECK (NOT (published_at IS NOT NULL AND dead_lettered_at IS NOT NULL)),
  CONSTRAINT chk_outbox_events_attempts CHECK (attempts >= 0)
);

COMMENT ON TABLE outbox.events IS 'Domain events recorded with the change that raised them, until a relay dispatches them';
COMMENT ON COLUMN outbox.events.id IS 'Event ID (evt_ prefixed ULID); handlers use it to recognize an event they already handled';
COMMENT ON COLUMN outbox.events.type IS 'Event name including its contract version, such as delivery.v1.application.created';
COMMENT ON COLUMN outbox.events.organization_id IS 'Organization the raising object belongs to; no cross-capability foreign key';
COMMENT ON COLUMN outbox.events.aggregate_type IS 'Domain type that raised the event, such as delivery.Application';
COMMENT ON COLUMN outbox.events.aggregate_id IS 'ID of the domain object that raised the event';
COMMENT ON COLUMN outbox.events.correlation_id IS 'ID of the event that started the chain of reactions';
COMMENT ON COLUMN outbox.events.causation_id IS 'ID of the event whose handling raised this one, or NULL for an event that started a chain';
COMMENT ON COLUMN outbox.events.occurred_at IS 'When the domain object raised the event';
COMMENT ON COLUMN outbox.events.payload IS 'The event as JSON, as its type declares it';
COMMENT ON COLUMN outbox.events.recorded_at IS 'When the event was written to the outbox';
COMMENT ON COLUMN outbox.events.published_at IS 'When a relay last dispatched the event successfully; NULL until then';
COMMENT ON COLUMN outbox.events.attempts IS 'How many deliveries have failed';
COMMENT ON COLUMN outbox.events.next_attempt_at IS 'Earliest time a failed event may be claimed again; NULL for a new event';
COMMENT ON COLUMN outbox.events.claimed_until IS 'Lease held by a relay that is dispatching the event; NULL when unclaimed';
COMMENT ON COLUMN outbox.events.dead_lettered_at IS 'When the event exhausted its retries; NULL unless dead-lettered';
COMMENT ON COLUMN outbox.events.last_error IS 'The most recent delivery error';

-- Claim reads pending entries in occurred_at order and skips the rest.
CREATE INDEX IF NOT EXISTS idx_outbox_events_pending ON outbox.events(occurred_at, id)
  WHERE published_at IS NULL AND dead_lettered_at IS NULL;
-- Purge deletes published entries older than the retention window.
CREATE INDEX IF NOT EXISTS idx_outbox_events_published ON outbox.events(published_at)
  WHERE published_at IS NOT NULL;
-- Operators inspect dead-lettered entries by time.
CREATE INDEX IF NOT EXISTS idx_outbox_events_dead_lettered ON outbox.events(dead_lettered_at)
  WHERE dead_lettered_at IS NOT NULL;
-- Organization-scoped inspection of a tenant's events.
CREATE INDEX IF NOT EXISTS idx_outbox_events_organization ON outbox.events(organization_id, occurred_at DESC);

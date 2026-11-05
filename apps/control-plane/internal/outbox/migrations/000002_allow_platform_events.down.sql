UPDATE outbox.events SET organization_id = '' WHERE organization_id IS NULL;
ALTER TABLE outbox.events ALTER COLUMN organization_id SET NOT NULL;

COMMENT ON COLUMN outbox.events.organization_id IS 'Organization the raising object belongs to; no cross-capability foreign key';

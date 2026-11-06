-- Audit records: what changed, who changed it, and the authorization decision
-- that allowed it. A capability writes its record in the transaction that
-- makes the change, so a change never commits without its record. Records
-- are only ever inserted.
--
-- There are no foreign keys to other capabilities' tables: records outlive
-- what they describe, and IDs name principals and resources in IAM and
-- objects in whichever capability recorded them.
CREATE TABLE IF NOT EXISTS audit.records (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  type VARCHAR(255) NOT NULL,
  resource_id VARCHAR(255) NOT NULL,
  actor_principal_id VARCHAR(30) NULL,
  actor_kind VARCHAR(50) NOT NULL,
  actor_session_id VARCHAR(30) NULL,
  decision_id VARCHAR(30) NULL,
  approved_by_principal_id VARCHAR(30) NULL,
  before JSONB NULL,
  after JSONB NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Only Ivaldi itself acts without a principal.
  CONSTRAINT chk_audit_records_actor CHECK (actor_principal_id IS NOT NULL OR actor_kind = 'system')
);

COMMENT ON TABLE audit.records IS 'What changed, who changed it, and the decision that allowed it; insert only';
COMMENT ON COLUMN audit.records.id IS 'Record ID (aud_ prefixed ULID)';
COMMENT ON COLUMN audit.records.organization_id IS 'Organization the change was made in; no cross-capability foreign key';
COMMENT ON COLUMN audit.records.type IS 'What happened, named by the recording capability, such as delivery.environment.created';
COMMENT ON COLUMN audit.records.resource_id IS 'ID of the changed object, or the Organization ID for an Organization-level change';
COMMENT ON COLUMN audit.records.actor_principal_id IS 'IAM principal that made the change; NULL when Ivaldi itself made it (actor_kind system)';
COMMENT ON COLUMN audit.records.actor_kind IS 'Kind of the actor, such as user, service_account, or agent_session';
COMMENT ON COLUMN audit.records.actor_session_id IS 'Agent Session the actor acted in, if any';
COMMENT ON COLUMN audit.records.decision_id IS 'Authorizer decision that allowed the change, if any';
COMMENT ON COLUMN audit.records.approved_by_principal_id IS 'Human who approved the change, if it needed approval';
COMMENT ON COLUMN audit.records.before IS 'Snapshot of the object before the change; NULL for a creation';
COMMENT ON COLUMN audit.records.after IS 'Snapshot of the object after the change';
COMMENT ON COLUMN audit.records.occurred_at IS 'When the change was made';
COMMENT ON COLUMN audit.records.recorded_at IS 'When the record was written';

CREATE INDEX IF NOT EXISTS idx_audit_records_organization ON audit.records(organization_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_records_resource ON audit.records(organization_id, resource_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_records_actor ON audit.records(organization_id, actor_principal_id, occurred_at DESC);

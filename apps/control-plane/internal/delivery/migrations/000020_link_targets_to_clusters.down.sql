DROP INDEX IF EXISTS delivery.idx_targets_cluster_namespace;
DROP INDEX IF EXISTS delivery.idx_targets_cluster;

ALTER TABLE delivery.targets DROP CONSTRAINT IF EXISTS targets_cluster_fk;
ALTER TABLE delivery.targets DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE delivery.targets DROP COLUMN IF EXISTS can_observe;
ALTER TABLE delivery.targets DROP COLUMN IF EXISTS can_execute;

-- Agent assignments are not restored: the old per-Target enrolment is gone.
ALTER TABLE delivery.targets ADD COLUMN IF NOT EXISTS agent_principal_id VARCHAR(30) NULL;
ALTER TABLE delivery.targets ADD COLUMN IF NOT EXISTS last_reported_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_targets_agent
  ON delivery.targets(organization_id, agent_principal_id) WHERE agent_principal_id IS NOT NULL;

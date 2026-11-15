ALTER TABLE delivery.deployments DROP CONSTRAINT IF EXISTS fk_deployments_variable_snapshot;
ALTER TABLE delivery.deployments DROP COLUMN IF EXISTS variable_snapshot_id;

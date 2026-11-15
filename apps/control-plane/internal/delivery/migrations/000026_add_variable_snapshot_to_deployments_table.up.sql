ALTER TABLE delivery.deployments
  ADD COLUMN IF NOT EXISTS variable_snapshot_id VARCHAR(30) NULL;

ALTER TABLE delivery.deployments
  ADD CONSTRAINT fk_deployments_variable_snapshot
  FOREIGN KEY (organization_id, variable_snapshot_id) REFERENCES delivery.variable_snapshots (organization_id, id);

COMMENT ON COLUMN delivery.deployments.variable_snapshot_id IS 'ID of the revision of the release''s variables an executed deployment was requested with; NULL for observed deployments and releases that have none';

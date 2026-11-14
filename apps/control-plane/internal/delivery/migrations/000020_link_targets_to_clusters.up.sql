ALTER TABLE delivery.targets
  ADD COLUMN cluster_id VARCHAR(30) NULL,
  ADD COLUMN can_observe BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN can_execute BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE delivery.targets
  ADD CONSTRAINT targets_cluster_fk
  FOREIGN KEY (organization_id, cluster_id)
  REFERENCES delivery.clusters(organization_id, id);

CREATE INDEX IF NOT EXISTS idx_targets_cluster
  ON delivery.targets(organization_id, cluster_id) WHERE deleted_at IS NULL;

-- One active Target per namespace of a Cluster. Several Environments share a
-- Cluster through their own Targets, never through the same namespace: they
-- would deploy the same Application under one Helm release name and
-- double-report its workloads.
CREATE UNIQUE INDEX IF NOT EXISTS idx_targets_cluster_namespace
  ON delivery.targets(organization_id, cluster_id, (locator->>'namespace'))
  WHERE deleted_at IS NULL AND cluster_id IS NOT NULL;

COMMENT ON COLUMN delivery.targets.cluster_id IS 'ID of the cluster whose agent serves the target; its slug is locator.cluster; NULL only on development rows from before this migration, which do not load (make local_reset)';
COMMENT ON COLUMN delivery.targets.can_observe IS 'Whether the cluster''s agent last reported it can read workloads in the target''s namespace';
COMMENT ON COLUMN delivery.targets.can_execute IS 'Whether the cluster''s agent last reported it can apply releases in the target''s namespace';

-- Liveness is the Cluster's now, and a Target's agent is its Cluster's agent.
ALTER TABLE delivery.targets DROP COLUMN agent_principal_id;
ALTER TABLE delivery.targets DROP COLUMN last_reported_at;

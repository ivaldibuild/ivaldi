DROP INDEX IF EXISTS iam.idx_workload_enrollment_tokens_cluster;

ALTER TABLE iam.workload_enrollment_tokens RENAME COLUMN cluster_id TO target_id;
CREATE INDEX IF NOT EXISTS idx_workload_enrollment_tokens_target
  ON iam.workload_enrollment_tokens(organization_id, target_id);

-- An enrolment token names the Cluster whose agent will redeem it, not a
-- Target: one agent serves the whole Cluster.
ALTER TABLE iam.workload_enrollment_tokens RENAME COLUMN target_id TO cluster_id;

DROP INDEX IF EXISTS iam.idx_workload_enrollment_tokens_target;
CREATE INDEX IF NOT EXISTS idx_workload_enrollment_tokens_cluster
  ON iam.workload_enrollment_tokens(organization_id, cluster_id);

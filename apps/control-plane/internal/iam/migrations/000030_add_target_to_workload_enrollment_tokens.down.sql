DROP INDEX IF EXISTS iam.idx_workload_enrollment_tokens_target;

ALTER TABLE iam.workload_enrollment_tokens
  DROP COLUMN IF EXISTS credential_kind,
  DROP COLUMN IF EXISTS target_id;

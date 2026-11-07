ALTER TABLE iam.workload_credentials
  DROP CONSTRAINT IF EXISTS fk_workload_credentials_replaced_by;

DROP INDEX IF EXISTS iam.idx_workload_credentials_secret_hash;
DROP INDEX IF EXISTS iam.idx_workload_credentials_organization;
DROP INDEX IF EXISTS iam.idx_workload_credentials_active_identifier;

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_credentials_identifier
  ON iam.workload_credentials(kind, identifier);

ALTER TABLE iam.workload_credentials
  DROP COLUMN IF EXISTS replaced_by,
  DROP COLUMN IF EXISTS secret_hash;

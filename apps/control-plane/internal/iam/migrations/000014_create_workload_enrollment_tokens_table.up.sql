CREATE TABLE IF NOT EXISTS iam.workload_enrollment_tokens (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  token_hash BYTEA NOT NULL,
  pool VARCHAR(255) NOT NULL,
  created_by_principal_id VARCHAR(30) NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ NULL,
  used_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, created_by_principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, used_by_principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.workload_enrollment_tokens IS 'Single-use tokens a workload exchanges to enroll';
COMMENT ON COLUMN iam.workload_enrollment_tokens.id IS 'Primary key of the enrollment token (wet_ prefixed ULID)';
COMMENT ON COLUMN iam.workload_enrollment_tokens.organization_id IS 'ID of the organization the workload will enroll in';
COMMENT ON COLUMN iam.workload_enrollment_tokens.token_hash IS 'SHA-256 hash of the token; never the raw token';
COMMENT ON COLUMN iam.workload_enrollment_tokens.pool IS 'Pool the enrolled workload will serve';
COMMENT ON COLUMN iam.workload_enrollment_tokens.created_by_principal_id IS 'ID of the principal that issued the token';
COMMENT ON COLUMN iam.workload_enrollment_tokens.expires_at IS 'Timestamp after which the token can no longer be used';
COMMENT ON COLUMN iam.workload_enrollment_tokens.used_at IS 'Timestamp when the token was used; set at most once';
COMMENT ON COLUMN iam.workload_enrollment_tokens.used_by_principal_id IS 'ID of the workload principal that enrolled with the token';
COMMENT ON COLUMN iam.workload_enrollment_tokens.created_at IS 'Timestamp when the token was created';

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_enrollment_tokens_hash ON iam.workload_enrollment_tokens(token_hash);

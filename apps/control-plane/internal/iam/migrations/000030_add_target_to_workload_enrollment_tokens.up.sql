ALTER TABLE iam.workload_enrollment_tokens
  ADD COLUMN IF NOT EXISTS target_id VARCHAR(30) NULL,
  ADD COLUMN IF NOT EXISTS credential_kind VARCHAR(50) NOT NULL DEFAULT 'bearer_token';

COMMENT ON COLUMN iam.workload_enrollment_tokens.target_id IS 'Target a runtime agent will serve; no cross-capability foreign key';
COMMENT ON COLUMN iam.workload_enrollment_tokens.credential_kind IS 'Credential the enrolled workload will use: bearer_token, mtls_certificate, or kubernetes_service_account';

CREATE INDEX IF NOT EXISTS idx_workload_enrollment_tokens_target
  ON iam.workload_enrollment_tokens(organization_id, target_id)
  WHERE target_id IS NOT NULL;

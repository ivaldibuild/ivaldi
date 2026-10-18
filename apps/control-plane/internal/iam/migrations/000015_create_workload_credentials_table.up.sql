CREATE TABLE IF NOT EXISTS iam.workload_credentials (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  identifier TEXT NOT NULL,
  not_after TIMESTAMPTZ NULL,
  revoked_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.workload_credentials IS 'Credentials an enrolled workload authenticates with';
COMMENT ON COLUMN iam.workload_credentials.id IS 'Primary key of the credential (wcr_ prefixed ULID)';
COMMENT ON COLUMN iam.workload_credentials.organization_id IS 'ID of the organization the workload is enrolled in';
COMMENT ON COLUMN iam.workload_credentials.principal_id IS 'ID of the workload principal';
COMMENT ON COLUMN iam.workload_credentials.kind IS 'Credential kind, e.g., mtls_certificate, k8s_service_account';
COMMENT ON COLUMN iam.workload_credentials.identifier IS 'Certificate fingerprint, or issuer + subject';
COMMENT ON COLUMN iam.workload_credentials.not_after IS 'Timestamp after which the credential is no longer valid';
COMMENT ON COLUMN iam.workload_credentials.revoked_at IS 'Timestamp when the credential was revoked';
COMMENT ON COLUMN iam.workload_credentials.created_at IS 'Timestamp when the credential was registered';

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_credentials_identifier ON iam.workload_credentials(kind, identifier);
CREATE INDEX IF NOT EXISTS idx_workload_credentials_principal ON iam.workload_credentials(organization_id, principal_id);

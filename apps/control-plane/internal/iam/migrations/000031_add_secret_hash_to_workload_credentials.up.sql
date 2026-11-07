ALTER TABLE iam.workload_credentials
  ADD COLUMN IF NOT EXISTS secret_hash BYTEA NULL,
  ADD COLUMN IF NOT EXISTS replaced_by VARCHAR(30) NULL;

COMMENT ON COLUMN iam.workload_credentials.secret_hash IS 'SHA-256 hash of a bearer token; NULL for certificates and Kubernetes service accounts';
COMMENT ON COLUMN iam.workload_credentials.replaced_by IS 'ID of the credential that succeeded this one';

-- Superseded credentials stay for lineage, so (kind, identifier) cannot be
-- unique across all rows. A live row is unique, though: a request is
-- authenticated before its Organization is known, so an identity must be
-- unambiguous. Redeem revokes the Organization's previous credential for the
-- same identity first.
DROP INDEX IF EXISTS iam.idx_workload_credentials_identifier;

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_credentials_active_identifier
  ON iam.workload_credentials(kind, identifier)
  WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_credentials_organization
  ON iam.workload_credentials(organization_id, id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_credentials_secret_hash
  ON iam.workload_credentials(kind, secret_hash)
  WHERE secret_hash IS NOT NULL;

ALTER TABLE iam.workload_credentials
  ADD CONSTRAINT fk_workload_credentials_replaced_by
  FOREIGN KEY (organization_id, replaced_by)
  REFERENCES iam.workload_credentials(organization_id, id);

-- Platform workloads (ADR 0015) are workload principals that belong to no
-- Organization, such as the gateway. Every Organization-scoped query filters
-- on organization_id = ?, which never matches NULL, so they never appear in
-- an Organization's results.
ALTER TABLE iam.principals ALTER COLUMN organization_id DROP NOT NULL;
ALTER TABLE iam.principals ADD CONSTRAINT chk_principals_platform_workload
  CHECK (organization_id IS NOT NULL OR kind = 'workload');

ALTER TABLE iam.workloads ALTER COLUMN organization_id DROP NOT NULL;
ALTER TABLE iam.workload_credentials ALTER COLUMN organization_id DROP NOT NULL;

-- The composite (organization_id, principal_id) keys are not checked when
-- organization_id is NULL, so platform rows reference their principal by ID.
ALTER TABLE iam.workloads ADD CONSTRAINT fk_workloads_principal
  FOREIGN KEY (principal_id) REFERENCES iam.principals (id);
ALTER TABLE iam.workload_credentials ADD CONSTRAINT fk_workload_credentials_workload
  FOREIGN KEY (principal_id) REFERENCES iam.workloads (principal_id);

-- A public_key credential keeps the workload's public key; the private key
-- never leaves the workload.
ALTER TABLE iam.workload_credentials ADD COLUMN IF NOT EXISTS public_key BYTEA NULL;

-- Platform workloads are declared in configuration by name.
CREATE UNIQUE INDEX IF NOT EXISTS idx_principals_platform_name
  ON iam.principals(display_name) WHERE organization_id IS NULL;

COMMENT ON COLUMN iam.principals.organization_id IS 'Organization the principal belongs to, or NULL for a platform workload (ADR 0015)';
COMMENT ON COLUMN iam.workloads.organization_id IS 'Organization the workload belongs to, or NULL for a platform workload (ADR 0015)';
COMMENT ON COLUMN iam.workload_credentials.organization_id IS 'Organization of the credential''s workload, or NULL for a platform workload (ADR 0015)';
COMMENT ON COLUMN iam.workload_credentials.public_key IS 'DER-encoded (PKIX) public key of a public_key credential; identifier is its JWK thumbprint';

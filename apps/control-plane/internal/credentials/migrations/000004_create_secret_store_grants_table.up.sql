CREATE TABLE IF NOT EXISTS credentials.secret_store_grants (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  secret_store_id VARCHAR(30) NOT NULL REFERENCES credentials.secret_stores(id),
  deployment_id VARCHAR(30) NOT NULL,
  mode VARCHAR(30) NOT NULL,
  token_id VARCHAR(255) NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE credentials.secret_store_grants IS 'Access the control plane granted to a secret store for one deployment; never the credential granted';
COMMENT ON COLUMN credentials.secret_store_grants.id IS 'Primary key of the grant (ssg_ prefixed ULID)';
COMMENT ON COLUMN credentials.secret_store_grants.organization_id IS 'ID of the organization the store belongs to; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_store_grants.secret_store_id IS 'ID of the store access was granted to';
COMMENT ON COLUMN credentials.secret_store_grants.deployment_id IS 'ID of the deployment the grant is for; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_store_grants.mode IS 'The store''s access mode when it was granted, such as federated';
COMMENT ON COLUMN credentials.secret_store_grants.token_id IS 'What was granted, such as a federated JWT''s ID; never the credential';
COMMENT ON COLUMN credentials.secret_store_grants.expires_at IS 'Timestamp when what was granted expires';
COMMENT ON COLUMN credentials.secret_store_grants.revoked_at IS 'Timestamp when what was granted was revoked, where the store can revoke it';
COMMENT ON COLUMN credentials.secret_store_grants.created_at IS 'Timestamp when the grant was made';

CREATE INDEX IF NOT EXISTS idx_secret_store_grants_deployment ON credentials.secret_store_grants(organization_id, deployment_id, created_at);

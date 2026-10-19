CREATE TABLE IF NOT EXISTS version_control.connections (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  provider VARCHAR(50) NOT NULL,
  host_url TEXT NOT NULL,
  auth_kind VARCHAR(50) NOT NULL,
  credential_ref TEXT NULL,
  external_account_id VARCHAR(255) NOT NULL,
  external_account_name VARCHAR(255) NULL,
  webhook_route_key VARCHAR(512) NULL,
  webhook_secret_ref TEXT NULL,
  lookup_from VARCHAR(50) NOT NULL,
  lookup_pool VARCHAR(255) NULL,
  status VARCHAR(50) NOT NULL,
  created_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE version_control.connections IS 'How an organization reaches one version-control provider account, such as a GitHub App installation';
COMMENT ON COLUMN version_control.connections.id IS 'Primary key of the connection (vcn_ prefixed ULID)';
COMMENT ON COLUMN version_control.connections.organization_id IS 'ID of the organization the connection belongs to; no cross-capability foreign key';
COMMENT ON COLUMN version_control.connections.provider IS 'Provider, e.g., github, gitlab, bitbucket, gitea, azure_devops';
COMMENT ON COLUMN version_control.connections.host_url IS 'Address of the provider, e.g., https://github.com or a self-hosted server';
COMMENT ON COLUMN version_control.connections.auth_kind IS 'How Ivaldi authenticates, e.g., app_installation, oauth, access_token';
COMMENT ON COLUMN version_control.connections.credential_ref IS 'Credential reference for provider authentication (ADR 0004); never a raw secret';
COMMENT ON COLUMN version_control.connections.external_account_id IS 'Provider ID of the account or installation, e.g., a GitHub App installation ID';
COMMENT ON COLUMN version_control.connections.external_account_name IS 'Provider name of the account, e.g., the GitHub organization login';
COMMENT ON COLUMN version_control.connections.webhook_route_key IS 'Platform-wide key the gateway routes webhooks by; NULL if the connection receives none';
COMMENT ON COLUMN version_control.connections.webhook_secret_ref IS 'Credential reference for the webhook signing secret (ADR 0004); never a raw secret';
COMMENT ON COLUMN version_control.connections.lookup_from IS 'Where lookups run, e.g., control_plane, data_plane (ADR 0008)';
COMMENT ON COLUMN version_control.connections.lookup_pool IS 'Workload pool that runs lookups when lookup_from is data_plane';
COMMENT ON COLUMN version_control.connections.status IS 'Status of the connection, e.g., active, suspended, revoked';
COMMENT ON COLUMN version_control.connections.created_by_principal_id IS 'ID of the principal that created the connection; no cross-capability foreign key';
COMMENT ON COLUMN version_control.connections.created_at IS 'Timestamp when the connection was created';
COMMENT ON COLUMN version_control.connections.updated_at IS 'Timestamp when the connection was last updated';
COMMENT ON COLUMN version_control.connections.deleted_at IS 'Timestamp when the connection was soft-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_connections_organization ON version_control.connections(organization_id, id);
-- Platform-wide, not per tenant: every webhook must route to exactly one organization.
CREATE UNIQUE INDEX IF NOT EXISTS idx_connections_webhook_route ON version_control.connections(webhook_route_key) WHERE webhook_route_key IS NOT NULL AND deleted_at IS NULL;

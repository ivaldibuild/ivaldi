CREATE TABLE IF NOT EXISTS credentials.secret_stores (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  settings JSONB NOT NULL DEFAULT '{}',
  environment_ids TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  CONSTRAINT chk_secret_stores_settings_object CHECK (jsonb_typeof(settings) = 'object')
);

COMMENT ON TABLE credentials.secret_stores IS 'Secret managers a runtime agent reads secret variables from; holds where a store is, never a credential';
COMMENT ON COLUMN credentials.secret_stores.id IS 'Primary key of the secret store (sst_ prefixed ULID)';
COMMENT ON COLUMN credentials.secret_stores.organization_id IS 'ID of the organization the store belongs to; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_stores.workspace_id IS 'ID of the workspace that owns the store; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_stores.slug IS 'Slug of the store, unique among the workspace''s active stores';
COMMENT ON COLUMN credentials.secret_stores.display_name IS 'Display name of the store';
COMMENT ON COLUMN credentials.secret_stores.kind IS 'Secret manager, e.g., kubernetes, aws_secrets_manager, vault, doppler';
COMMENT ON COLUMN credentials.secret_stores.settings IS 'Non-secret settings for the kind, e.g., the AWS region, the Vault address and mount, the Secret holding a Doppler token';
COMMENT ON COLUMN credentials.secret_stores.environment_ids IS 'IDs of the environments whose deployments may read the store; empty means every environment in the workspace; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_stores.created_at IS 'Timestamp when the store was created';
COMMENT ON COLUMN credentials.secret_stores.updated_at IS 'Timestamp when the store was last updated';
COMMENT ON COLUMN credentials.secret_stores.archived_at IS 'Timestamp when the store was archived';

CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_stores_organization ON credentials.secret_stores(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_stores_workspace_slug ON credentials.secret_stores(organization_id, workspace_id, slug) WHERE archived_at IS NULL;

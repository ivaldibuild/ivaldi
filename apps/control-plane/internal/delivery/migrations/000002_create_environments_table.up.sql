CREATE TABLE IF NOT EXISTS delivery.environments (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  tier VARCHAR(50) NOT NULL,
  execution_enabled BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE delivery.environments IS 'Named stages in a workspace, such as staging or production; registered with IAM as resources under their workspace';
COMMENT ON COLUMN delivery.environments.id IS 'Primary key of the environment (env_ prefixed ULID); also its IAM resource ID';
COMMENT ON COLUMN delivery.environments.organization_id IS 'ID of the organization the environment belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.environments.workspace_id IS 'ID of the workspace that owns the environment; no cross-capability foreign key';
COMMENT ON COLUMN delivery.environments.slug IS 'Slug of the environment, unique within the workspace';
COMMENT ON COLUMN delivery.environments.display_name IS 'Display name of the environment';
COMMENT ON COLUMN delivery.environments.tier IS 'Tier policy rules can match on, e.g., development, staging, production, other';
COMMENT ON COLUMN delivery.environments.execution_enabled IS 'Whether Ivaldi may execute deployments here; when false the environment is tracked only';
COMMENT ON COLUMN delivery.environments.created_at IS 'Timestamp when the environment was created';
COMMENT ON COLUMN delivery.environments.updated_at IS 'Timestamp when the environment was last updated';
COMMENT ON COLUMN delivery.environments.deleted_at IS 'Timestamp when the environment was soft-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_environments_organization ON delivery.environments(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_environments_workspace_slug ON delivery.environments(organization_id, workspace_id, slug) WHERE deleted_at IS NULL;

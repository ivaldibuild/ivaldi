CREATE TABLE IF NOT EXISTS workspace.workspaces (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  is_default BOOLEAN NOT NULL DEFAULT false,
  created_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  -- The default workspace stays usable; move the default elsewhere before archiving it.
  CONSTRAINT chk_workspaces_default_not_archived CHECK (NOT (is_default AND archived_at IS NOT NULL))
);

COMMENT ON TABLE workspace.workspaces IS 'Spaces in an organization where related work happens; registered with IAM as resources under the organization';
COMMENT ON COLUMN workspace.workspaces.id IS 'Primary key of the workspace (wsp_ prefixed ULID); also its IAM resource ID';
COMMENT ON COLUMN workspace.workspaces.organization_id IS 'ID of the organization the workspace belongs to; no cross-capability foreign key';
COMMENT ON COLUMN workspace.workspaces.slug IS 'Slug of the workspace, unique among the organization''s active workspaces';
COMMENT ON COLUMN workspace.workspaces.display_name IS 'Display name of the workspace';
COMMENT ON COLUMN workspace.workspaces.description IS 'Description of the workspace';
COMMENT ON COLUMN workspace.workspaces.is_default IS 'Whether this is the organization''s default workspace, provisioned with the organization; exactly one per organization';
COMMENT ON COLUMN workspace.workspaces.created_by_principal_id IS 'ID of the principal that created the workspace; no cross-capability foreign key';
COMMENT ON COLUMN workspace.workspaces.created_at IS 'Timestamp when the workspace was created';
COMMENT ON COLUMN workspace.workspaces.updated_at IS 'Timestamp when the workspace was last updated';
COMMENT ON COLUMN workspace.workspaces.archived_at IS 'Timestamp when the workspace was archived; workspaces are never hard-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_organization ON workspace.workspaces(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_slug ON workspace.workspaces(organization_id, slug) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_default ON workspace.workspaces(organization_id) WHERE is_default;

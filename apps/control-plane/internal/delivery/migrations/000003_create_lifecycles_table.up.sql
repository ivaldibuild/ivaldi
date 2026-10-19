CREATE TABLE IF NOT EXISTS delivery.lifecycles (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE delivery.lifecycles IS 'Ordered paths through a workspace''s environments that releases move along; shared by many channels';
COMMENT ON COLUMN delivery.lifecycles.id IS 'Primary key of the lifecycle (lfc_ prefixed ULID)';
COMMENT ON COLUMN delivery.lifecycles.organization_id IS 'ID of the organization the lifecycle belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.lifecycles.workspace_id IS 'ID of the workspace that owns the lifecycle; no cross-capability foreign key';
COMMENT ON COLUMN delivery.lifecycles.slug IS 'Slug of the lifecycle, unique within the workspace';
COMMENT ON COLUMN delivery.lifecycles.display_name IS 'Display name of the lifecycle';
COMMENT ON COLUMN delivery.lifecycles.description IS 'Description of when the lifecycle is used';
COMMENT ON COLUMN delivery.lifecycles.created_at IS 'Timestamp when the lifecycle was created';
COMMENT ON COLUMN delivery.lifecycles.updated_at IS 'Timestamp when the lifecycle was last updated';
COMMENT ON COLUMN delivery.lifecycles.deleted_at IS 'Timestamp when the lifecycle was soft-deleted; not allowed while an active channel uses it';

CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycles_organization ON delivery.lifecycles(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycles_workspace_slug ON delivery.lifecycles(organization_id, workspace_id, slug) WHERE deleted_at IS NULL;

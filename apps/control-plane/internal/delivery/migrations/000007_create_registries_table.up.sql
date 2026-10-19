CREATE TABLE IF NOT EXISTS delivery.registries (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  url TEXT NOT NULL,
  credential_ref TEXT NULL,
  lookup_from VARCHAR(50) NOT NULL,
  lookup_pool VARCHAR(255) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE delivery.registries IS 'Places artifacts are stored, used only for lookups; clusters pull images themselves';
COMMENT ON COLUMN delivery.registries.id IS 'Primary key of the registry (reg_ prefixed ULID)';
COMMENT ON COLUMN delivery.registries.organization_id IS 'ID of the organization the registry belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.registries.workspace_id IS 'ID of the workspace that owns the registry; no cross-capability foreign key';
COMMENT ON COLUMN delivery.registries.slug IS 'Slug of the registry, unique within the workspace';
COMMENT ON COLUMN delivery.registries.display_name IS 'Display name of the registry';
COMMENT ON COLUMN delivery.registries.kind IS 'Registry protocol, e.g., oci';
COMMENT ON COLUMN delivery.registries.url IS 'Address of the registry';
COMMENT ON COLUMN delivery.registries.credential_ref IS 'Credential reference for registry authentication (ADR 0004); never a raw secret';
COMMENT ON COLUMN delivery.registries.lookup_from IS 'Where lookups run, e.g., control_plane, data_plane; data_plane registries are never contacted from the control plane';
COMMENT ON COLUMN delivery.registries.lookup_pool IS 'Workload pool that runs lookups when lookup_from is data_plane';
COMMENT ON COLUMN delivery.registries.created_at IS 'Timestamp when the registry was created';
COMMENT ON COLUMN delivery.registries.updated_at IS 'Timestamp when the registry was last updated';
COMMENT ON COLUMN delivery.registries.deleted_at IS 'Timestamp when the registry was soft-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_registries_organization ON delivery.registries(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_registries_workspace_slug ON delivery.registries(organization_id, workspace_id, slug) WHERE deleted_at IS NULL;

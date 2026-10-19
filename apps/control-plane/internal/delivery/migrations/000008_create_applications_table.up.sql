CREATE TABLE IF NOT EXISTS delivery.applications (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  project_id VARCHAR(30) NOT NULL,
  owner_team_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  source_id VARCHAR(30) NULL,
  workload_selector JSONB NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE delivery.applications IS 'Deployable units inside a project, each released and deployed on its own';
COMMENT ON COLUMN delivery.applications.id IS 'Primary key of the application (app_ prefixed ULID)';
COMMENT ON COLUMN delivery.applications.organization_id IS 'ID of the organization the application belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.applications.workspace_id IS 'ID of the project''s workspace, copied from the project; no cross-capability foreign key';
COMMENT ON COLUMN delivery.applications.project_id IS 'ID of the project the application belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.applications.owner_team_id IS 'ID of the team responsible for the application (ADR 0007); grants no access; no cross-capability foreign key';
COMMENT ON COLUMN delivery.applications.slug IS 'Slug of the application, unique within the project';
COMMENT ON COLUMN delivery.applications.display_name IS 'Display name of the application';
COMMENT ON COLUMN delivery.applications.source_id IS 'ID of the source the application is usually built from; no cross-capability foreign key';
COMMENT ON COLUMN delivery.applications.workload_selector IS 'Runtime-specific selector for the application''s workloads; NULL means the label ivaldi.build/application=<id>';
COMMENT ON COLUMN delivery.applications.created_at IS 'Timestamp when the application was created';
COMMENT ON COLUMN delivery.applications.updated_at IS 'Timestamp when the application was last updated';
COMMENT ON COLUMN delivery.applications.archived_at IS 'Timestamp when the application was archived; applications are never hard-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_applications_organization ON delivery.applications(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_applications_project_slug ON delivery.applications(organization_id, project_id, slug) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_applications_workspace ON delivery.applications(organization_id, workspace_id);
CREATE INDEX IF NOT EXISTS idx_applications_owner_team ON delivery.applications(organization_id, owner_team_id);

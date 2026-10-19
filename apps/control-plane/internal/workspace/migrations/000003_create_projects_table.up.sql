CREATE TABLE IF NOT EXISTS workspace.projects (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  owner_team_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  created_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspace.workspaces (organization_id, id)
);

COMMENT ON TABLE workspace.projects IS 'Bodies of work in a workspace; registered with IAM as resources under their workspace';
COMMENT ON COLUMN workspace.projects.id IS 'Primary key of the project (prj_ prefixed ULID); also its IAM resource ID';
COMMENT ON COLUMN workspace.projects.organization_id IS 'ID of the organization the project belongs to; no cross-capability foreign key';
COMMENT ON COLUMN workspace.projects.workspace_id IS 'ID of the workspace the project belongs to; never changes, because other capabilities tie records to it';
COMMENT ON COLUMN workspace.projects.owner_team_id IS 'ID of the team responsible for the project and the default owner of its new applications (ADR 0007); grants no access; no cross-capability foreign key';
COMMENT ON COLUMN workspace.projects.slug IS 'Slug of the project, unique among the workspace''s active projects';
COMMENT ON COLUMN workspace.projects.display_name IS 'Display name of the project';
COMMENT ON COLUMN workspace.projects.description IS 'Description of the project';
COMMENT ON COLUMN workspace.projects.created_by_principal_id IS 'ID of the principal that created the project; no cross-capability foreign key';
COMMENT ON COLUMN workspace.projects.created_at IS 'Timestamp when the project was created';
COMMENT ON COLUMN workspace.projects.updated_at IS 'Timestamp when the project was last updated';
COMMENT ON COLUMN workspace.projects.archived_at IS 'Timestamp when the project was archived; projects are never hard-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_organization ON workspace.projects(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_slug ON workspace.projects(organization_id, workspace_id, slug) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_workspace ON workspace.projects(organization_id, workspace_id);
CREATE INDEX IF NOT EXISTS idx_projects_owner_team ON workspace.projects(organization_id, owner_team_id);

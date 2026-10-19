CREATE TABLE IF NOT EXISTS version_control.sources (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  connection_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NOT NULL,
  project_id VARCHAR(30) NOT NULL,
  external_repo_id VARCHAR(255) NOT NULL,
  repository_key VARCHAR(768) NOT NULL,
  full_name VARCHAR(512) NOT NULL,
  default_branch VARCHAR(255) NULL,
  visibility VARCHAR(50) NOT NULL,
  clone_url TEXT NULL,
  web_url TEXT NULL,
  created_by_principal_id VARCHAR(30) NULL,
  upstream_archived_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, connection_id) REFERENCES version_control.connections (organization_id, id)
);

COMMENT ON TABLE version_control.sources IS 'Repositories connected through a connection, each under one project; registered with IAM as resources under their project';
COMMENT ON COLUMN version_control.sources.id IS 'Primary key of the source (src_ prefixed ULID); also its IAM resource ID';
COMMENT ON COLUMN version_control.sources.organization_id IS 'ID of the organization the source belongs to; no cross-capability foreign key';
COMMENT ON COLUMN version_control.sources.connection_id IS 'ID of the connection the repository is reached through';
COMMENT ON COLUMN version_control.sources.workspace_id IS 'ID of the project''s workspace, copied from the project; no cross-capability foreign key';
COMMENT ON COLUMN version_control.sources.project_id IS 'ID of the project the source belongs to; no cross-capability foreign key';
COMMENT ON COLUMN version_control.sources.external_repo_id IS 'Provider''s stable repository ID; unlike the name, it survives renames and transfers';
COMMENT ON COLUMN version_control.sources.repository_key IS 'Provider, host, and repository ID combined, e.g., github:github.com:123456; unique among the organization''s active sources';
COMMENT ON COLUMN version_control.sources.full_name IS 'Current repository name, e.g., acme/checkout; kept up to date from provider events';
COMMENT ON COLUMN version_control.sources.default_branch IS 'Current default branch, e.g., main; kept up to date from provider events';
COMMENT ON COLUMN version_control.sources.visibility IS 'Repository visibility, e.g., public, private, internal';
COMMENT ON COLUMN version_control.sources.clone_url IS 'URL the data plane clones the repository from';
COMMENT ON COLUMN version_control.sources.web_url IS 'URL of the repository in the provider''s web interface';
COMMENT ON COLUMN version_control.sources.created_by_principal_id IS 'ID of the principal that connected the source; no cross-capability foreign key';
COMMENT ON COLUMN version_control.sources.upstream_archived_at IS 'Timestamp when the provider archived the repository';
COMMENT ON COLUMN version_control.sources.created_at IS 'Timestamp when the source was connected';
COMMENT ON COLUMN version_control.sources.updated_at IS 'Timestamp when the source was last updated';
COMMENT ON COLUMN version_control.sources.archived_at IS 'Timestamp when the source was disconnected from Ivaldi; sources are never hard-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_organization ON version_control.sources(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_repository ON version_control.sources(organization_id, repository_key) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sources_project ON version_control.sources(organization_id, project_id);
CREATE INDEX IF NOT EXISTS idx_sources_connection ON version_control.sources(organization_id, connection_id);

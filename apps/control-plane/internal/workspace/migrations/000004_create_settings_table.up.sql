-- Lets settings check that a project belongs to the workspace they name.
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_workspace_id ON workspace.projects(organization_id, workspace_id, id);

CREATE TABLE IF NOT EXISTS workspace.settings (
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NULL,
  project_id VARCHAR(30) NULL,
  key VARCHAR(255) NOT NULL,
  value JSONB NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  updated_by_principal_id VARCHAR(30) NULL,
  policy_decision_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- The level is implied by which IDs are set: neither (organization), workspace, or workspace and project.
  CONSTRAINT chk_settings_project_has_workspace CHECK (project_id IS NULL OR workspace_id IS NOT NULL),
  -- Keys are namespaced by the capability that defines them, e.g., delivery.default_lifecycle.
  CONSTRAINT chk_settings_key_format CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
  -- Settings are small configuration values, not documents or data.
  CONSTRAINT chk_settings_value_size CHECK (octet_length(value::text) <= 16384),
  CONSTRAINT chk_settings_version CHECK (version >= 1),
  FOREIGN KEY (organization_id, workspace_id) REFERENCES workspace.workspaces (organization_id, id),
  FOREIGN KEY (organization_id, workspace_id, project_id) REFERENCES workspace.projects (organization_id, workspace_id, id)
);

COMMENT ON TABLE workspace.settings IS 'Setting values at organization, workspace, or project level; definitions, defaults, and validation are registered in Go by the capability that owns each key';
COMMENT ON COLUMN workspace.settings.organization_id IS 'ID of the organization; on its own, the setting applies to the whole organization; no cross-capability foreign key';
COMMENT ON COLUMN workspace.settings.workspace_id IS 'ID of the workspace the setting applies to; NULL for an organization-level setting';
COMMENT ON COLUMN workspace.settings.project_id IS 'ID of the project the setting applies to; NULL unless it is a project-level setting';
COMMENT ON COLUMN workspace.settings.key IS 'Setting key, namespaced by the defining capability, e.g., delivery.default_lifecycle';
COMMENT ON COLUMN workspace.settings.value IS 'Setting value as JSON, validated by the defining capability; never a secret, only credential references';
COMMENT ON COLUMN workspace.settings.version IS 'Incremented on every change; updates compare it to avoid overwriting a concurrent change';
COMMENT ON COLUMN workspace.settings.updated_by_principal_id IS 'ID of the principal that last changed the setting; no cross-capability foreign key';
COMMENT ON COLUMN workspace.settings.policy_decision_id IS 'ID of the authorizer decision for the last change, including any human approval; agent sessions always need one (ADR 0007); no cross-capability foreign key';
COMMENT ON COLUMN workspace.settings.created_at IS 'Timestamp when the setting was first set at this level';
COMMENT ON COLUMN workspace.settings.updated_at IS 'Timestamp when the setting was last changed';

-- One value per key and level. NULLS NOT DISTINCT makes the organization- and workspace-level rows unique too.
CREATE UNIQUE INDEX IF NOT EXISTS idx_settings_scope_key ON workspace.settings(organization_id, workspace_id, project_id, key) NULLS NOT DISTINCT;
CREATE INDEX IF NOT EXISTS idx_settings_key ON workspace.settings(organization_id, key);

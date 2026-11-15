CREATE TABLE IF NOT EXISTS delivery.variables (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  project_id VARCHAR(30) NULL,
  application_id VARCHAR(30) NULL,
  name VARCHAR(253) NOT NULL,
  description TEXT NULL,
  kind VARCHAR(50) NOT NULL,
  values_path VARCHAR(512) NULL,
  deliver_as VARCHAR(50) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  CONSTRAINT chk_variables_owner CHECK ((project_id IS NULL) <> (application_id IS NULL)),
  FOREIGN KEY (organization_id, application_id) REFERENCES delivery.applications (organization_id, id)
);

COMMENT ON TABLE delivery.variables IS 'A named piece of configuration a project''s applications, or one application, deploy with; its values are in variable_values';
COMMENT ON COLUMN delivery.variables.id IS 'Primary key of the variable (var_ prefixed ULID)';
COMMENT ON COLUMN delivery.variables.organization_id IS 'ID of the organization the variable belongs to';
COMMENT ON COLUMN delivery.variables.project_id IS 'ID of the project that owns the variable, when an application does not; no cross-capability foreign key';
COMMENT ON COLUMN delivery.variables.application_id IS 'ID of the application that owns the variable, when a project does not';
COMMENT ON COLUMN delivery.variables.name IS 'Name of the variable, unique within its owner whatever the case, e.g., Database.Host';
COMMENT ON COLUMN delivery.variables.description IS 'What the variable is for';
COMMENT ON COLUMN delivery.variables.kind IS 'What its values hold, e.g., text, secret';
COMMENT ON COLUMN delivery.variables.values_path IS 'Dotted path in the chart''s values the variable is written to; NULL when it is only used through bindings';
COMMENT ON COLUMN delivery.variables.deliver_as IS 'How a secret variable reaches the chart, e.g., secret, value; NULL for text variables';
COMMENT ON COLUMN delivery.variables.created_at IS 'Timestamp when the variable was created';
COMMENT ON COLUMN delivery.variables.updated_at IS 'Timestamp when the variable or one of its values was last changed';
COMMENT ON COLUMN delivery.variables.archived_at IS 'Timestamp when the variable was archived';

CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_organization ON delivery.variables(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_project_name ON delivery.variables(organization_id, project_id, lower(name)) WHERE project_id IS NOT NULL AND archived_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_variables_application_name ON delivery.variables(organization_id, application_id, lower(name)) WHERE application_id IS NOT NULL AND archived_at IS NULL;

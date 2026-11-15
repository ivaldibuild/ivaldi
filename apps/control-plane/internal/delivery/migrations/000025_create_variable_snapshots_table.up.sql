CREATE TABLE IF NOT EXISTS delivery.variable_snapshots (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  release_id VARCHAR(30) NOT NULL,
  revision INTEGER NOT NULL,
  status VARCHAR(50) NOT NULL,
  variables JSONB NOT NULL DEFAULT '[]',
  taken_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_variable_snapshots_revision CHECK (revision >= 1),
  CONSTRAINT chk_variable_snapshots_variables_array CHECK (jsonb_typeof(variables) = 'array'),
  FOREIGN KEY (organization_id, application_id, release_id) REFERENCES delivery.releases (organization_id, application_id, id)
);

COMMENT ON TABLE delivery.variable_snapshots IS 'One revision of the variables a release deploys with, taken when it was created or its variables were refreshed; its content never changes';
COMMENT ON COLUMN delivery.variable_snapshots.id IS 'Primary key of the revision (vsn_ prefixed ULID)';
COMMENT ON COLUMN delivery.variable_snapshots.organization_id IS 'ID of the organization the revision belongs to';
COMMENT ON COLUMN delivery.variable_snapshots.application_id IS 'ID of the application, copied from the release';
COMMENT ON COLUMN delivery.variable_snapshots.release_id IS 'ID of the release whose variables these are';
COMMENT ON COLUMN delivery.variable_snapshots.revision IS 'Number of the revision within its release, starting at 1';
COMMENT ON COLUMN delivery.variable_snapshots.status IS 'Whether deployments use it, e.g., current, superseded';
COMMENT ON COLUMN delivery.variable_snapshots.variables IS 'The application''s variables and its project''s as they were, each with its source and every scoped value; secrets as references, never values';
COMMENT ON COLUMN delivery.variable_snapshots.taken_by_principal_id IS 'ID of the principal that created or refreshed the release; NULL for an observed release; no cross-capability foreign key';
COMMENT ON COLUMN delivery.variable_snapshots.created_at IS 'Timestamp when the revision was taken';
COMMENT ON COLUMN delivery.variable_snapshots.updated_at IS 'Timestamp when its status last changed';

CREATE UNIQUE INDEX IF NOT EXISTS idx_variable_snapshots_organization ON delivery.variable_snapshots(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_variable_snapshots_revision ON delivery.variable_snapshots(organization_id, release_id, revision);
-- A release has at most one current revision.
CREATE UNIQUE INDEX IF NOT EXISTS idx_variable_snapshots_current ON delivery.variable_snapshots(organization_id, release_id) WHERE status = 'current';

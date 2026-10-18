CREATE TABLE IF NOT EXISTS iam.resources (
  id varchar(30) PRIMARY KEY,
  organization_id varchar(30) NOT NULL REFERENCES iam.organizations(id),
  resource_type varchar(50) NOT NULL,
  parent_id varchar(30) NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz NULL,
  CONSTRAINT chk_resources_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)
);

COMMENT ON TABLE iam.resources IS 'Table storing resource details';
COMMENT ON COLUMN iam.resources.id IS 'Primary key of the resource; the same ID as the entity it mirrors';
COMMENT ON COLUMN iam.resources.organization_id IS 'ID of the organization the resource belongs to';
COMMENT ON COLUMN iam.resources.resource_type IS 'Type of the resource, e.g., organization, workspace, project, source';
COMMENT ON COLUMN iam.resources.parent_id IS 'ID of the parent resource, if any';
COMMENT ON COLUMN iam.resources.created_at IS 'Timestamp when the resource was created';
COMMENT ON COLUMN iam.resources.updated_at IS 'Timestamp when the resource was last updated';
COMMENT ON COLUMN iam.resources.deleted_at IS 'Timestamp when the resource was soft-deleted; grants on it stop applying';

CREATE UNIQUE INDEX IF NOT EXISTS idx_resources_organization ON iam.resources(organization_id, id);

CREATE INDEX IF NOT EXISTS idx_resources_parent ON iam.resources(organization_id, parent_id);

ALTER TABLE iam.resources
  ADD CONSTRAINT fk_resources_organization_parent
  FOREIGN KEY (organization_id, parent_id) REFERENCES iam.resources(organization_id, id);

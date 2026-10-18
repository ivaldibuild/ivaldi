CREATE TABLE IF NOT EXISTS iam.groups (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  name VARCHAR(255) NOT NULL,
  source VARCHAR(50) NOT NULL,
  external_id VARCHAR(255) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.groups IS 'Manual or SCIM-provisioned groups of principals';
COMMENT ON COLUMN iam.groups.id IS 'Primary key of the group (grp_ prefixed ULID)';
COMMENT ON COLUMN iam.groups.organization_id IS 'ID of the organization the group belongs to';
COMMENT ON COLUMN iam.groups.name IS 'Name of the group, unique within the organization';
COMMENT ON COLUMN iam.groups.source IS 'Where the group is managed, e.g., manual, scim';
COMMENT ON COLUMN iam.groups.external_id IS 'ID of the group in the provisioning system, if any';
COMMENT ON COLUMN iam.groups.created_at IS 'Timestamp when the group was created';
COMMENT ON COLUMN iam.groups.updated_at IS 'Timestamp when the group was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_organization ON iam.groups(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_organization_name ON iam.groups(organization_id, name);

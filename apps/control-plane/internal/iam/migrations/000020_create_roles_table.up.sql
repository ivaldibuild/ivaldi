CREATE TABLE IF NOT EXISTS iam.roles (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NULL REFERENCES iam.organizations (id),
  key VARCHAR(100) NOT NULL,
  name VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.roles IS 'Roles; built-in roles have no organization, and their permissions are defined in Go';
COMMENT ON COLUMN iam.roles.id IS 'Primary key of the role (rol_ prefixed ULID)';
COMMENT ON COLUMN iam.roles.organization_id IS 'ID of the organization a custom role belongs to; NULL for built-in roles';
COMMENT ON COLUMN iam.roles.key IS 'Stable key code refers to, e.g., owner, admin';
COMMENT ON COLUMN iam.roles.name IS 'Display name of the role';
COMMENT ON COLUMN iam.roles.created_at IS 'Timestamp when the role was created';

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_organization_key ON iam.roles(organization_id, key) NULLS NOT DISTINCT;

-- Built-in roles use fixed IDs so every installation agrees on them; code looks roles up by key.
INSERT INTO iam.roles (id, organization_id, key, name) VALUES
  ('rol_00000000000000000000000001', NULL, 'owner', 'Owner'),
  ('rol_00000000000000000000000002', NULL, 'admin', 'Admin'),
  ('rol_00000000000000000000000003', NULL, 'member', 'Member'),
  ('rol_00000000000000000000000004', NULL, 'viewer', 'Viewer');

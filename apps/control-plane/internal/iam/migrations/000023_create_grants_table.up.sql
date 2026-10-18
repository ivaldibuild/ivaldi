CREATE TABLE IF NOT EXISTS iam.grants (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NULL,
  group_id VARCHAR(30) NULL,
  role_id VARCHAR(30) NOT NULL REFERENCES iam.roles (id),
  resource_id VARCHAR(30) NOT NULL,
  granted_by_principal_id VARCHAR(30) NOT NULL,
  expires_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_grants_one_grantee CHECK (num_nonnulls(principal_id, group_id) = 1),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, group_id) REFERENCES iam.groups (organization_id, id),
  FOREIGN KEY (organization_id, resource_id) REFERENCES iam.resources (organization_id, id),
  FOREIGN KEY (organization_id, granted_by_principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.grants IS 'Gives a role to exactly one principal or group on one resource; grants only allow';
COMMENT ON COLUMN iam.grants.id IS 'Primary key of the grant (grt_ prefixed ULID)';
COMMENT ON COLUMN iam.grants.organization_id IS 'ID of the organization the grant belongs to';
COMMENT ON COLUMN iam.grants.principal_id IS 'ID of the grantee principal; set when group_id is not';
COMMENT ON COLUMN iam.grants.group_id IS 'ID of the grantee group; set when principal_id is not';
COMMENT ON COLUMN iam.grants.role_id IS 'ID of the granted role';
COMMENT ON COLUMN iam.grants.resource_id IS 'ID of the resource the grant applies to, and to everything beneath it';
COMMENT ON COLUMN iam.grants.granted_by_principal_id IS 'ID of the principal that created the grant';
COMMENT ON COLUMN iam.grants.expires_at IS 'Timestamp after which the grant stops applying';
COMMENT ON COLUMN iam.grants.created_at IS 'Timestamp when the grant was created';

CREATE UNIQUE INDEX IF NOT EXISTS idx_grants_unique ON iam.grants(principal_id, group_id, role_id, resource_id) NULLS NOT DISTINCT;
CREATE INDEX IF NOT EXISTS idx_grants_principal ON iam.grants(organization_id, principal_id) WHERE principal_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_grants_group ON iam.grants(organization_id, group_id) WHERE group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_grants_resource ON iam.grants(organization_id, resource_id);

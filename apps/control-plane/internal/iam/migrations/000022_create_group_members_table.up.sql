CREATE TABLE IF NOT EXISTS iam.group_members (
  organization_id VARCHAR(30) NOT NULL,
  group_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, principal_id),
  FOREIGN KEY (organization_id, group_id) REFERENCES iam.groups (organization_id, id),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.group_members IS 'Membership of principals in groups';
COMMENT ON COLUMN iam.group_members.organization_id IS 'ID of the organization the group belongs to';
COMMENT ON COLUMN iam.group_members.group_id IS 'ID of the group';
COMMENT ON COLUMN iam.group_members.principal_id IS 'ID of the member principal';
COMMENT ON COLUMN iam.group_members.created_at IS 'Timestamp when the principal was added';

CREATE INDEX IF NOT EXISTS idx_group_members_principal ON iam.group_members(organization_id, principal_id);

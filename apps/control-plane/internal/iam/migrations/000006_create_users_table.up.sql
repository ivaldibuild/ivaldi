CREATE TABLE IF NOT EXISTS iam.users (
  principal_id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  account_id VARCHAR(30) NOT NULL REFERENCES iam.accounts (id),
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.users IS 'A human principal: one account''s membership in one organization';
COMMENT ON COLUMN iam.users.principal_id IS 'ID of the principal (kind user) this row details';
COMMENT ON COLUMN iam.users.organization_id IS 'ID of the organization the membership belongs to';
COMMENT ON COLUMN iam.users.account_id IS 'ID of the platform account that holds this membership';
COMMENT ON COLUMN iam.users.joined_at IS 'Timestamp when the account joined the organization';

-- One membership per account per organization; also serves account -> organizations lookups.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_account_organization ON iam.users(account_id, organization_id);

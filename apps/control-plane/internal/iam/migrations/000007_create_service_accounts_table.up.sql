CREATE TABLE IF NOT EXISTS iam.service_accounts (
  principal_id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_by_principal_id VARCHAR(30) NOT NULL,
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, created_by_principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.service_accounts IS 'A non-human integration principal that authenticates with API tokens';
COMMENT ON COLUMN iam.service_accounts.principal_id IS 'ID of the principal (kind service_account) this row details';
COMMENT ON COLUMN iam.service_accounts.organization_id IS 'ID of the organization the service account belongs to';
COMMENT ON COLUMN iam.service_accounts.description IS 'What the service account is used for';
COMMENT ON COLUMN iam.service_accounts.created_by_principal_id IS 'ID of the principal that created the service account';

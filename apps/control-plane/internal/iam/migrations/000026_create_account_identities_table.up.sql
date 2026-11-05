CREATE TABLE IF NOT EXISTS iam.account_identities (
  id VARCHAR(30) PRIMARY KEY,
  account_id VARCHAR(30) NOT NULL REFERENCES iam.accounts (id),
  provider VARCHAR(50) NOT NULL,
  subject VARCHAR(512) NOT NULL,
  last_login_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.account_identities IS 'Links a platform account to a platform sign-in provider subject, for managed-cloud sign-up and sign-in before any organization is chosen';
COMMENT ON COLUMN iam.account_identities.id IS 'Primary key of the account identity (aid_ prefixed ULID)';
COMMENT ON COLUMN iam.account_identities.account_id IS 'ID of the account the subject signs in as';
COMMENT ON COLUMN iam.account_identities.provider IS 'Platform provider asserting the subject, e.g., google, github';
COMMENT ON COLUMN iam.account_identities.subject IS 'OIDC sub, or GitHub numeric user ID';
COMMENT ON COLUMN iam.account_identities.last_login_at IS 'Timestamp of the last sign-in through this identity';
COMMENT ON COLUMN iam.account_identities.created_at IS 'Timestamp when the identity was linked';

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_identities_subject ON iam.account_identities(provider, subject);
CREATE INDEX IF NOT EXISTS idx_account_identities_account ON iam.account_identities(account_id);

CREATE TABLE IF NOT EXISTS iam.accounts (
  id VARCHAR(30) PRIMARY KEY,
  email VARCHAR(320) NOT NULL,
  email_verified_at TIMESTAMPTZ NULL,
  display_name VARCHAR(255) NOT NULL,
  status VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.accounts IS 'Platform-scoped person identity; each organization membership is a row in iam.users';
COMMENT ON COLUMN iam.accounts.id IS 'Primary key of the account (acc_ prefixed ULID)';
COMMENT ON COLUMN iam.accounts.email IS 'Sign-in email, unique across the platform (case-insensitive)';
COMMENT ON COLUMN iam.accounts.email_verified_at IS 'Timestamp when the email was verified';
COMMENT ON COLUMN iam.accounts.display_name IS 'Default display name of the person';
COMMENT ON COLUMN iam.accounts.status IS 'Status of the account, e.g., active, suspended, deactivated';
COMMENT ON COLUMN iam.accounts.created_at IS 'Timestamp when the account was created';
COMMENT ON COLUMN iam.accounts.updated_at IS 'Timestamp when the account was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_email ON iam.accounts(lower(email));

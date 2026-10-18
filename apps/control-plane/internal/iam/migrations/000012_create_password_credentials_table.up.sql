CREATE TABLE IF NOT EXISTS iam.password_credentials (
  account_id VARCHAR(30) PRIMARY KEY REFERENCES iam.accounts (id),
  password_hash TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.password_credentials IS 'Password hashes for the built-in provider; platform-scoped, one per account';
COMMENT ON COLUMN iam.password_credentials.account_id IS 'ID of the account the password belongs to';
COMMENT ON COLUMN iam.password_credentials.password_hash IS 'argon2id hash in PHC string format; never a raw password';
COMMENT ON COLUMN iam.password_credentials.updated_at IS 'Timestamp when the password was last changed';

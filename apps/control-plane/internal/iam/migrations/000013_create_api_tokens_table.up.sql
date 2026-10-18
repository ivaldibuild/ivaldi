CREATE TABLE IF NOT EXISTS iam.api_tokens (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  name VARCHAR(255) NOT NULL,
  token_prefix VARCHAR(16) NOT NULL,
  token_hash BYTEA NOT NULL,
  expires_at TIMESTAMPTZ NULL,
  last_used_at TIMESTAMPTZ NULL,
  revoked_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.api_tokens IS 'Long-lived tokens for users and service accounts; acts with the owner''s grants';
COMMENT ON COLUMN iam.api_tokens.id IS 'Primary key of the token (tok_ prefixed ULID)';
COMMENT ON COLUMN iam.api_tokens.organization_id IS 'ID of the organization the token is valid in';
COMMENT ON COLUMN iam.api_tokens.principal_id IS 'ID of the principal that owns the token';
COMMENT ON COLUMN iam.api_tokens.name IS 'Name given to the token';
COMMENT ON COLUMN iam.api_tokens.token_prefix IS 'Short display prefix shown in the UI to identify the token';
COMMENT ON COLUMN iam.api_tokens.token_hash IS 'SHA-256 hash of the token; never the raw token';
COMMENT ON COLUMN iam.api_tokens.expires_at IS 'Timestamp after which the token is no longer valid';
COMMENT ON COLUMN iam.api_tokens.last_used_at IS 'Timestamp when the token was last used';
COMMENT ON COLUMN iam.api_tokens.revoked_at IS 'Timestamp when the token was revoked';
COMMENT ON COLUMN iam.api_tokens.created_at IS 'Timestamp when the token was created';

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_hash ON iam.api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_principal ON iam.api_tokens(organization_id, principal_id);

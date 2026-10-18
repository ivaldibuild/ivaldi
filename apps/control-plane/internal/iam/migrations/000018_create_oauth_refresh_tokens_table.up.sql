CREATE TABLE IF NOT EXISTS iam.oauth_refresh_tokens (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  client_id VARCHAR(30) NOT NULL REFERENCES iam.oauth_clients (id),
  family_id VARCHAR(30) NOT NULL,
  token_hash BYTEA NOT NULL,
  resource TEXT NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT '{}',
  expires_at TIMESTAMPTZ NOT NULL,
  replaced_by VARCHAR(30) NULL REFERENCES iam.oauth_refresh_tokens (id),
  revoked_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.oauth_refresh_tokens IS 'Rotating OAuth refresh tokens; reuse of a rotated token revokes its family';
COMMENT ON COLUMN iam.oauth_refresh_tokens.id IS 'Primary key of the refresh token (ort_ prefixed ULID)';
COMMENT ON COLUMN iam.oauth_refresh_tokens.organization_id IS 'ID of the organization the token is valid in';
COMMENT ON COLUMN iam.oauth_refresh_tokens.principal_id IS 'ID of the principal the token was issued to';
COMMENT ON COLUMN iam.oauth_refresh_tokens.client_id IS 'ID of the OAuth client the token was issued to';
COMMENT ON COLUMN iam.oauth_refresh_tokens.family_id IS 'Groups a rotation chain; app-generated, not its own table';
COMMENT ON COLUMN iam.oauth_refresh_tokens.token_hash IS 'SHA-256 hash of the token; never the raw token';
COMMENT ON COLUMN iam.oauth_refresh_tokens.resource IS 'RFC 8707 resource indicator';
COMMENT ON COLUMN iam.oauth_refresh_tokens.scopes IS 'Granted scopes';
COMMENT ON COLUMN iam.oauth_refresh_tokens.expires_at IS 'Timestamp after which the token is no longer valid';
COMMENT ON COLUMN iam.oauth_refresh_tokens.replaced_by IS 'ID of the token this one was rotated into';
COMMENT ON COLUMN iam.oauth_refresh_tokens.revoked_at IS 'Timestamp when the token was revoked';
COMMENT ON COLUMN iam.oauth_refresh_tokens.created_at IS 'Timestamp when the token was issued';

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_hash ON iam.oauth_refresh_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_tokens_family ON iam.oauth_refresh_tokens(family_id);

CREATE TABLE IF NOT EXISTS iam.oauth_authorization_codes (
  code_hash BYTEA PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  client_id VARCHAR(30) NOT NULL REFERENCES iam.oauth_clients (id),
  redirect_uri TEXT NOT NULL,
  code_challenge VARCHAR(128) NOT NULL,
  resource TEXT NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT '{}',
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.oauth_authorization_codes IS 'Short-lived, single-use OAuth authorization codes';
COMMENT ON COLUMN iam.oauth_authorization_codes.code_hash IS 'SHA-256 hash of the code; never the raw code';
COMMENT ON COLUMN iam.oauth_authorization_codes.organization_id IS 'ID of the organization the consent was given in';
COMMENT ON COLUMN iam.oauth_authorization_codes.principal_id IS 'ID of the principal that consented';
COMMENT ON COLUMN iam.oauth_authorization_codes.client_id IS 'ID of the OAuth client the code was issued to';
COMMENT ON COLUMN iam.oauth_authorization_codes.redirect_uri IS 'Redirect URI the code was issued for';
COMMENT ON COLUMN iam.oauth_authorization_codes.code_challenge IS 'PKCE S256 code challenge';
COMMENT ON COLUMN iam.oauth_authorization_codes.resource IS 'RFC 8707 resource indicator';
COMMENT ON COLUMN iam.oauth_authorization_codes.scopes IS 'Granted scopes';
COMMENT ON COLUMN iam.oauth_authorization_codes.expires_at IS 'Timestamp after which the code can no longer be exchanged';
COMMENT ON COLUMN iam.oauth_authorization_codes.used_at IS 'Timestamp when the code was exchanged; set at most once';

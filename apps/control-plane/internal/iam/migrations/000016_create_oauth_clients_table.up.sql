CREATE TABLE IF NOT EXISTS iam.oauth_clients (
  id VARCHAR(30) PRIMARY KEY,
  client_id VARCHAR(2048) NOT NULL,
  registration VARCHAR(50) NOT NULL,
  name VARCHAR(255) NOT NULL,
  redirect_uris TEXT[] NOT NULL,
  metadata_url TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE iam.oauth_clients IS 'MCP and API OAuth clients; platform-scoped because registration happens before a tenant is known';
COMMENT ON COLUMN iam.oauth_clients.id IS 'Primary key of the client (ocl_ prefixed ULID)';
COMMENT ON COLUMN iam.oauth_clients.client_id IS 'OAuth client_id; a URL for client metadata documents';
COMMENT ON COLUMN iam.oauth_clients.registration IS 'How the client was registered, e.g., first_party, dynamic, metadata_document';
COMMENT ON COLUMN iam.oauth_clients.name IS 'Name of the client shown on the consent screen';
COMMENT ON COLUMN iam.oauth_clients.redirect_uris IS 'Allowed redirect URIs';
COMMENT ON COLUMN iam.oauth_clients.metadata_url IS 'URL of the client metadata document, if any';
COMMENT ON COLUMN iam.oauth_clients.created_at IS 'Timestamp when the client was registered';
COMMENT ON COLUMN iam.oauth_clients.last_used_at IS 'Timestamp when the client was last used';

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_clients_client_id ON iam.oauth_clients(client_id);

ALTER TABLE credentials.secret_stores ADD COLUMN IF NOT EXISTS credential_set_at TIMESTAMPTZ NULL;
ALTER TABLE credentials.secret_stores ADD COLUMN IF NOT EXISTS credential_set_by VARCHAR(30) NULL;

COMMENT ON COLUMN credentials.secret_stores.credential_set_at IS 'Timestamp when a brokered store''s minting credential was set; null while it has none';
COMMENT ON COLUMN credentials.secret_stores.credential_set_by IS 'ID of the principal who set a brokered store''s minting credential; no cross-capability foreign key';

CREATE TABLE IF NOT EXISTS credentials.data_keys (
  organization_id VARCHAR(30) PRIMARY KEY,
  key_id VARCHAR(255) NOT NULL,
  wrapped_key BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE credentials.data_keys IS 'Each organization''s data key, wrapped by a key-encryption key that is never stored here (ADR 0004)';
COMMENT ON COLUMN credentials.data_keys.organization_id IS 'ID of the organization the data key encrypts for; no cross-capability foreign key';
COMMENT ON COLUMN credentials.data_keys.key_id IS 'ID of the key-encryption key that wrapped the data key';
COMMENT ON COLUMN credentials.data_keys.wrapped_key IS 'The data key, encrypted with the key-encryption key';
COMMENT ON COLUMN credentials.data_keys.created_at IS 'Timestamp when the data key was made';

CREATE TABLE IF NOT EXISTS credentials.secret_store_credentials (
  secret_store_id VARCHAR(30) PRIMARY KEY REFERENCES credentials.secret_stores(id) ON DELETE CASCADE,
  organization_id VARCHAR(30) NOT NULL,
  ciphertext BYTEA NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE credentials.secret_store_credentials IS 'Brokered stores'' minting credentials, sealed with their organization''s data key; never in the clear';
COMMENT ON COLUMN credentials.secret_store_credentials.secret_store_id IS 'ID of the store the credential mints for';
COMMENT ON COLUMN credentials.secret_store_credentials.organization_id IS 'ID of the organization whose data key sealed the credential; no cross-capability foreign key';
COMMENT ON COLUMN credentials.secret_store_credentials.ciphertext IS 'The credential, sealed with AES-256-GCM and bound to the store';
COMMENT ON COLUMN credentials.secret_store_credentials.updated_at IS 'Timestamp when the credential was last set';

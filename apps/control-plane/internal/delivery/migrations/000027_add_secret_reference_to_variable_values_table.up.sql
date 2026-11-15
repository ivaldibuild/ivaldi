ALTER TABLE delivery.variable_values
  ADD COLUMN IF NOT EXISTS store_id VARCHAR(30) NULL,
  ADD COLUMN IF NOT EXISTS path VARCHAR(512) NULL,
  ADD COLUMN IF NOT EXISTS field VARCHAR(255) NULL,
  ADD COLUMN IF NOT EXISTS version VARCHAR(255) NULL;

-- A value is text or a reference to a secret, never both.
ALTER TABLE delivery.variable_values
  ADD CONSTRAINT chk_variable_values_content
  CHECK ((store_id IS NULL AND path IS NULL AND field IS NULL AND version IS NULL)
      OR (store_id IS NOT NULL AND path IS NOT NULL AND text_value IS NULL));

COMMENT ON COLUMN delivery.variable_values.store_id IS 'ID of the secret store a secret variable''s value is kept in; no cross-capability foreign key';
COMMENT ON COLUMN delivery.variable_values.path IS 'Where the secret is in its store, e.g., a Secret name, an AWS secret ID, a Vault path; may contain #{Name} bindings';
COMMENT ON COLUMN delivery.variable_values.field IS 'One key of a map or JSON secret; NULL for the whole value';
COMMENT ON COLUMN delivery.variable_values.version IS 'Version of the secret to deploy; NULL for the latest when deployed';

CREATE INDEX IF NOT EXISTS idx_variable_values_store ON delivery.variable_values(organization_id, store_id) WHERE store_id IS NOT NULL;

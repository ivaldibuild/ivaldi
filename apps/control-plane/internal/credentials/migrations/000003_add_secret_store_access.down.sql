ALTER TABLE credentials.secret_stores DROP CONSTRAINT IF EXISTS chk_secret_stores_access_object;
ALTER TABLE credentials.secret_stores DROP COLUMN IF EXISTS access;

COMMENT ON COLUMN credentials.secret_stores.settings IS 'Non-secret settings for the kind, e.g., the AWS region, the Vault address and mount, the Secret holding a Doppler token';

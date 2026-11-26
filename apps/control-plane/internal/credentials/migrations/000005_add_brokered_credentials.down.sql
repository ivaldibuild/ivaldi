DROP TABLE IF EXISTS credentials.secret_store_credentials;
DROP TABLE IF EXISTS credentials.data_keys;
ALTER TABLE credentials.secret_stores DROP COLUMN IF EXISTS credential_set_by;
ALTER TABLE credentials.secret_stores DROP COLUMN IF EXISTS credential_set_at;

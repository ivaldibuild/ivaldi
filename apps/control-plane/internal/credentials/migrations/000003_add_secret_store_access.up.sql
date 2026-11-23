-- How a store is opened, apart from where it is (ADR 0017). The application
-- owns the shape of settings and access for each kind; the database stores
-- them as objects. An empty access is read as agent mode.
ALTER TABLE credentials.secret_stores ADD COLUMN IF NOT EXISTS access JSONB NOT NULL DEFAULT '{}';
ALTER TABLE credentials.secret_stores ADD CONSTRAINT chk_secret_stores_access_object CHECK (jsonb_typeof(access) = 'object');

COMMENT ON COLUMN credentials.secret_stores.settings IS 'Where the store is; its shape depends on the kind and is checked by the application';
COMMENT ON COLUMN credentials.secret_stores.access IS 'How the store is opened: its mode and that mode''s settings, shaped by the application; never a credential';

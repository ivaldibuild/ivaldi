ALTER TABLE version_control.sources DROP COLUMN IF EXISTS provider_updated_at;
ALTER TABLE version_control.connections DROP COLUMN IF EXISTS provider_updated_at;

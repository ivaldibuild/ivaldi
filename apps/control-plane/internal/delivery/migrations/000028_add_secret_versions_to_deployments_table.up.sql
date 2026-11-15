ALTER TABLE delivery.deployments
  ADD COLUMN IF NOT EXISTS secret_versions JSONB NOT NULL DEFAULT '[]';

ALTER TABLE delivery.deployments
  ADD CONSTRAINT chk_deployments_secret_versions_array CHECK (jsonb_typeof(secret_versions) = 'array');

COMMENT ON COLUMN delivery.deployments.secret_versions IS 'For each secret the agent resolved: its variable, store, path, field, the version it read, and a keyed fingerprint whose key stays in the cluster; never a value';

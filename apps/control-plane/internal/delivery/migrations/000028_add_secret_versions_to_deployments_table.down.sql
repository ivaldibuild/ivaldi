ALTER TABLE delivery.deployments DROP CONSTRAINT IF EXISTS chk_deployments_secret_versions_array;
ALTER TABLE delivery.deployments DROP COLUMN IF EXISTS secret_versions;

ALTER TABLE delivery.deployments
  ADD COLUMN IF NOT EXISTS secret_paths JSONB NULL;

COMMENT ON COLUMN delivery.deployments.secret_paths IS 'Where the deployment reads each secret from, by variable name, with the bindings filled in when it was requested; NULL for a deployment requested before paths were fixed, whose paths are filled in when assigned';

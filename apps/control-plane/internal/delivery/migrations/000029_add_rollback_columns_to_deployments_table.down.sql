DROP INDEX IF EXISTS delivery.idx_deployments_release_target_succeeded;
ALTER TABLE delivery.deployments DROP CONSTRAINT IF EXISTS fk_deployments_restores_deployment;
ALTER TABLE delivery.deployments
  DROP COLUMN IF EXISTS restores_deployment_id,
  DROP COLUMN IF EXISTS secrets_latest;

ALTER TABLE delivery.deployments
  ADD COLUMN IF NOT EXISTS restores_deployment_id VARCHAR(30) NULL,
  ADD COLUMN IF NOT EXISTS secrets_latest BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE delivery.deployments
  ADD CONSTRAINT fk_deployments_restores_deployment
  FOREIGN KEY (organization_id, restores_deployment_id) REFERENCES delivery.deployments (organization_id, id);

COMMENT ON COLUMN delivery.deployments.restores_deployment_id IS 'For a rollback, ID of the deployment whose variables and secret versions it replays; NULL when there was nothing recorded to replay';
COMMENT ON COLUMN delivery.deployments.secrets_latest IS 'Whether a rollback was asked to read the latest secrets instead of the versions the restored deployment used';

-- The last deployment of a release that became healthy on a target, which a rollback replays.
CREATE INDEX IF NOT EXISTS idx_deployments_release_target_succeeded ON delivery.deployments(organization_id, release_id, target_id, succeeded_at DESC) WHERE succeeded_at IS NOT NULL;

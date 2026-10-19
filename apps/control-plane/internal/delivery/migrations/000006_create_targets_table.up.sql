CREATE TABLE IF NOT EXISTS delivery.targets (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  environment_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  runtime VARCHAR(50) NOT NULL,
  locator JSONB NOT NULL,
  agent_principal_id VARCHAR(30) NULL,
  last_reported_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, environment_id) REFERENCES delivery.environments (organization_id, id)
);

COMMENT ON TABLE delivery.targets IS 'Concrete places an environment runs, such as a Kubernetes cluster and namespace';
COMMENT ON COLUMN delivery.targets.id IS 'Primary key of the target (tgt_ prefixed ULID)';
COMMENT ON COLUMN delivery.targets.organization_id IS 'ID of the organization the target belongs to';
COMMENT ON COLUMN delivery.targets.environment_id IS 'ID of the environment the target belongs to';
COMMENT ON COLUMN delivery.targets.slug IS 'Slug of the target, unique within its environment';
COMMENT ON COLUMN delivery.targets.display_name IS 'Display name of the target';
COMMENT ON COLUMN delivery.targets.runtime IS 'Runtime provider, e.g., kubernetes';
COMMENT ON COLUMN delivery.targets.locator IS 'Non-secret, runtime-specific address, e.g., cluster identity and namespace; validated by the runtime provider';
COMMENT ON COLUMN delivery.targets.agent_principal_id IS 'ID of the IAM workload principal of the runtime agent serving the target; NULL until assigned; no cross-capability foreign key';
COMMENT ON COLUMN delivery.targets.last_reported_at IS 'Timestamp when the agent last reported on the target';
COMMENT ON COLUMN delivery.targets.created_at IS 'Timestamp when the target was created';
COMMENT ON COLUMN delivery.targets.updated_at IS 'Timestamp when the target was last updated';
COMMENT ON COLUMN delivery.targets.deleted_at IS 'Timestamp when the target was soft-deleted';

CREATE UNIQUE INDEX IF NOT EXISTS idx_targets_organization ON delivery.targets(organization_id, id);
-- Lets deployments check that their environment is the target's environment.
CREATE UNIQUE INDEX IF NOT EXISTS idx_targets_environment ON delivery.targets(organization_id, environment_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_targets_environment_slug ON delivery.targets(organization_id, environment_id, slug) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_targets_agent ON delivery.targets(organization_id, agent_principal_id) WHERE agent_principal_id IS NOT NULL;

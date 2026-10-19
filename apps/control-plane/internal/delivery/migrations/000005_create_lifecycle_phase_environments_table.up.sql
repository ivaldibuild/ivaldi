CREATE TABLE IF NOT EXISTS delivery.lifecycle_phase_environments (
  organization_id VARCHAR(30) NOT NULL,
  lifecycle_id VARCHAR(30) NOT NULL,
  phase_id VARCHAR(30) NOT NULL,
  environment_id VARCHAR(30) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (phase_id, environment_id),
  -- An environment appears at most once in a lifecycle.
  CONSTRAINT uq_lifecycle_phase_environments_lifecycle UNIQUE (lifecycle_id, environment_id),
  -- Including lifecycle_id ties the row to the phase's own lifecycle.
  FOREIGN KEY (organization_id, lifecycle_id, phase_id) REFERENCES delivery.lifecycle_phases (organization_id, lifecycle_id, id) ON DELETE CASCADE,
  FOREIGN KEY (organization_id, environment_id) REFERENCES delivery.environments (organization_id, id)
);

COMMENT ON TABLE delivery.lifecycle_phase_environments IS 'The environments in each lifecycle phase';
COMMENT ON COLUMN delivery.lifecycle_phase_environments.organization_id IS 'ID of the organization the lifecycle belongs to';
COMMENT ON COLUMN delivery.lifecycle_phase_environments.lifecycle_id IS 'ID of the phase''s lifecycle; carried so an environment is unique within a lifecycle';
COMMENT ON COLUMN delivery.lifecycle_phase_environments.phase_id IS 'ID of the phase';
COMMENT ON COLUMN delivery.lifecycle_phase_environments.environment_id IS 'ID of an environment in the phase; must be in the lifecycle''s workspace';
COMMENT ON COLUMN delivery.lifecycle_phase_environments.created_at IS 'Timestamp when the environment was added to the phase';

CREATE INDEX IF NOT EXISTS idx_lifecycle_phase_environments_environment ON delivery.lifecycle_phase_environments(organization_id, environment_id);

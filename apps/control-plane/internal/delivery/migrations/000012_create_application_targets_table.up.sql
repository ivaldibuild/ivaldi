CREATE TABLE IF NOT EXISTS delivery.application_targets (
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  target_id VARCHAR(30) NOT NULL,
  source VARCHAR(50) NOT NULL,
  apply_config JSONB NULL,
  current_deployment_id VARCHAR(30) NULL,
  current_release_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (application_id, target_id),
  FOREIGN KEY (organization_id, application_id) REFERENCES delivery.applications (organization_id, id),
  FOREIGN KEY (organization_id, target_id) REFERENCES delivery.targets (organization_id, id)
);

COMMENT ON TABLE delivery.application_targets IS 'The targets an application runs on, and what is running there now';
COMMENT ON COLUMN delivery.application_targets.organization_id IS 'ID of the organization the placement belongs to';
COMMENT ON COLUMN delivery.application_targets.application_id IS 'ID of the application';
COMMENT ON COLUMN delivery.application_targets.target_id IS 'ID of the target; must be in the application''s workspace';
COMMENT ON COLUMN delivery.application_targets.source IS 'How the placement became known, e.g., configured, observed';
COMMENT ON COLUMN delivery.application_targets.apply_config IS 'How to apply a release when executing: which artifact holds the deployment definition, and value references; used only for executed deployments';
COMMENT ON COLUMN delivery.application_targets.current_deployment_id IS 'ID of the deployment currently running; foreign key added with the deployments table';
COMMENT ON COLUMN delivery.application_targets.current_release_id IS 'ID of the release currently running; foreign key added with the releases table';
COMMENT ON COLUMN delivery.application_targets.created_at IS 'Timestamp when the placement was created';
COMMENT ON COLUMN delivery.application_targets.updated_at IS 'Timestamp when the placement was last updated';

-- Lets deployments check that a placement exists for their application and target.
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_targets_organization ON delivery.application_targets(organization_id, application_id, target_id);
CREATE INDEX IF NOT EXISTS idx_application_targets_target ON delivery.application_targets(organization_id, target_id);

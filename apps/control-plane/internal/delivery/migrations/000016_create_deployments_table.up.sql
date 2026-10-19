CREATE TABLE IF NOT EXISTS delivery.deployments (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  release_id VARCHAR(30) NOT NULL,
  environment_id VARCHAR(30) NOT NULL,
  target_id VARCHAR(30) NOT NULL,
  source VARCHAR(50) NOT NULL,
  reason VARCHAR(50) NOT NULL,
  status VARCHAR(50) NOT NULL,
  previous_deployment_id VARCHAR(30) NULL,
  initiated_by_principal_id VARCHAR(30) NULL,
  session_id VARCHAR(30) NULL,
  work_item_id VARCHAR(30) NULL,
  external_ref JSONB NULL,
  policy_decision_id VARCHAR(30) NULL,
  findings JSONB NOT NULL DEFAULT '[]',
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ NULL,
  finished_at TIMESTAMPTZ NULL,
  succeeded_at TIMESTAMPTZ NULL,
  status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_deployments_findings_array CHECK (jsonb_typeof(findings) = 'array'),
  -- Each composite key ties a copied column to its source: the release to the
  -- application, the target to the environment, and the pair to a placement.
  FOREIGN KEY (organization_id, application_id, release_id) REFERENCES delivery.releases (organization_id, application_id, id),
  FOREIGN KEY (organization_id, environment_id, target_id) REFERENCES delivery.targets (organization_id, environment_id, id),
  FOREIGN KEY (organization_id, application_id, target_id) REFERENCES delivery.application_targets (organization_id, application_id, target_id)
);

COMMENT ON TABLE delivery.deployments IS 'One release of an application applied to one target, whether Ivaldi executed it or observed it';
COMMENT ON COLUMN delivery.deployments.id IS 'Primary key of the deployment (dep_ prefixed ULID)';
COMMENT ON COLUMN delivery.deployments.organization_id IS 'ID of the organization the deployment belongs to';
COMMENT ON COLUMN delivery.deployments.application_id IS 'ID of the application, copied from the release';
COMMENT ON COLUMN delivery.deployments.release_id IS 'ID of the release deployed';
COMMENT ON COLUMN delivery.deployments.environment_id IS 'ID of the environment, copied from the target';
COMMENT ON COLUMN delivery.deployments.target_id IS 'ID of the target deployed to';
COMMENT ON COLUMN delivery.deployments.source IS 'Who applied it, e.g., executed (Ivaldi), observed (another tool)';
COMMENT ON COLUMN delivery.deployments.reason IS 'Why it was made, e.g., rollout, rollback, redeploy';
COMMENT ON COLUMN delivery.deployments.status IS 'Status following the transitions in pkg/release, e.g., pending, awaiting_approval, applying, progressing, healthy, degraded, failed, superseded';
COMMENT ON COLUMN delivery.deployments.previous_deployment_id IS 'ID of the deployment this one replaced on the same target';
COMMENT ON COLUMN delivery.deployments.initiated_by_principal_id IS 'ID of the principal that requested an executed deployment; no cross-capability foreign key';
COMMENT ON COLUMN delivery.deployments.session_id IS 'ID of the session that requested it, if any; no cross-capability foreign key';
COMMENT ON COLUMN delivery.deployments.work_item_id IS 'ID of the work item it serves, if any; no cross-capability foreign key';
COMMENT ON COLUMN delivery.deployments.external_ref IS 'For observed deployments, the deployer and its own revision';
COMMENT ON COLUMN delivery.deployments.policy_decision_id IS 'ID of the authorizer decision, including any approval; evaluated after the fact for observed deployments; no cross-capability foreign key';
COMMENT ON COLUMN delivery.deployments.findings IS 'Rules an observed deployment broke, e.g., outside_lifecycle, phase_not_open, would_require_approval, would_deny; always empty for executed deployments';
COMMENT ON COLUMN delivery.deployments.requested_at IS 'Timestamp when the deployment was requested or first observed';
COMMENT ON COLUMN delivery.deployments.started_at IS 'Timestamp when applying or the rollout started';
COMMENT ON COLUMN delivery.deployments.finished_at IS 'Timestamp when the rollout ended, or when the deployment was rejected or cancelled; NULL while in flight';
COMMENT ON COLUMN delivery.deployments.succeeded_at IS 'Timestamp when it first became healthy; what lifecycle progression counts, even after it is superseded';
COMMENT ON COLUMN delivery.deployments.status_changed_at IS 'Timestamp of the last status change';
COMMENT ON COLUMN delivery.deployments.created_at IS 'Timestamp when the deployment was created';
COMMENT ON COLUMN delivery.deployments.updated_at IS 'Timestamp when the deployment was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_organization ON delivery.deployments(organization_id, id);
-- Lets a placement's current deployment be checked against the placement's application and target.
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_placement ON delivery.deployments(organization_id, application_id, target_id, id);
-- At most one deployment per application and target is in flight.
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_in_flight ON delivery.deployments(organization_id, application_id, target_id) WHERE finished_at IS NULL;
-- Lifecycle progression: which environments a release has succeeded in.
CREATE INDEX IF NOT EXISTS idx_deployments_succeeded ON delivery.deployments(organization_id, release_id, environment_id) WHERE succeeded_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deployments_target_history ON delivery.deployments(organization_id, target_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_deployments_environment_history ON delivery.deployments(organization_id, environment_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_deployments_session ON delivery.deployments(organization_id, session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deployments_work_item ON delivery.deployments(organization_id, work_item_id) WHERE work_item_id IS NOT NULL;

-- Added after the indexes because it refers to this table's own (organization_id, id) key.
ALTER TABLE delivery.deployments
  ADD CONSTRAINT fk_deployments_previous_deployment
  FOREIGN KEY (organization_id, previous_deployment_id) REFERENCES delivery.deployments (organization_id, id);

ALTER TABLE delivery.application_targets
  ADD CONSTRAINT fk_application_targets_current_deployment
  FOREIGN KEY (organization_id, application_id, target_id, current_deployment_id) REFERENCES delivery.deployments (organization_id, application_id, target_id, id);

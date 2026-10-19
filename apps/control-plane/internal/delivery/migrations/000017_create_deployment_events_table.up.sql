CREATE TABLE IF NOT EXISTS delivery.deployment_events (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  deployment_id VARCHAR(30) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  status VARCHAR(50) NULL,
  detail JSONB NULL,
  report_id VARCHAR(255) NULL,
  reported_by_principal_id VARCHAR(30) NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, deployment_id) REFERENCES delivery.deployments (organization_id, id)
);

COMMENT ON TABLE delivery.deployment_events IS 'Append-only history of a deployment''s status changes, progress, and health reports';
COMMENT ON COLUMN delivery.deployment_events.id IS 'Primary key of the event (dpe_ prefixed ULID)';
COMMENT ON COLUMN delivery.deployment_events.organization_id IS 'ID of the organization the deployment belongs to';
COMMENT ON COLUMN delivery.deployment_events.deployment_id IS 'ID of the deployment the event is about';
COMMENT ON COLUMN delivery.deployment_events.kind IS 'Kind of event, e.g., status_changed, progress, health';
COMMENT ON COLUMN delivery.deployment_events.status IS 'Status the deployment moved to, if the event changed it';
COMMENT ON COLUMN delivery.deployment_events.detail IS 'Runtime-specific detail of the event';
COMMENT ON COLUMN delivery.deployment_events.report_id IS 'Agent-supplied ID of the report; unique per deployment so a retried report is stored once; NULL for control-plane events';
COMMENT ON COLUMN delivery.deployment_events.reported_by_principal_id IS 'ID of the principal that reported the event, e.g., the target''s agent; no cross-capability foreign key';
COMMENT ON COLUMN delivery.deployment_events.occurred_at IS 'Timestamp when the event happened, by the reporter''s clock';
COMMENT ON COLUMN delivery.deployment_events.recorded_at IS 'Timestamp when the event was stored';

CREATE UNIQUE INDEX IF NOT EXISTS idx_deployment_events_report ON delivery.deployment_events(deployment_id, report_id) WHERE report_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deployment_events_deployment ON delivery.deployment_events(organization_id, deployment_id, occurred_at);

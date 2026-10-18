CREATE TABLE IF NOT EXISTS iam.workloads (
  principal_id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  component VARCHAR(50) NOT NULL,
  pool VARCHAR(255) NOT NULL,
  enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.workloads IS 'An enrolled data-plane component';
COMMENT ON COLUMN iam.workloads.principal_id IS 'ID of the principal (kind workload) this row details';
COMMENT ON COLUMN iam.workloads.organization_id IS 'ID of the organization the workload is enrolled in';
COMMENT ON COLUMN iam.workloads.component IS 'Component type, e.g., worker, indexer, gateway, helm_agent';
COMMENT ON COLUMN iam.workloads.pool IS 'Pool the workload serves';
COMMENT ON COLUMN iam.workloads.enrolled_at IS 'Timestamp when the workload enrolled';
COMMENT ON COLUMN iam.workloads.last_seen_at IS 'Timestamp when the workload last authenticated';

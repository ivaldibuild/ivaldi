CREATE TABLE IF NOT EXISTS delivery.clusters (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  runtime VARCHAR(50) NOT NULL,
  agent_principal_id VARCHAR(30) NULL,
  agent_capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  agent_last_failure JSONB NULL,
  last_reported_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE delivery.clusters IS 'Kubernetes clusters an organization deploys to; one runtime agent serves a cluster and every target in it';
COMMENT ON COLUMN delivery.clusters.id IS 'Primary key of the cluster (cls_ prefixed ULID)';
COMMENT ON COLUMN delivery.clusters.organization_id IS 'ID of the organization the cluster belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.clusters.slug IS 'Slug of the cluster, unique within the organization and immutable after creation';
COMMENT ON COLUMN delivery.clusters.display_name IS 'Display name of the cluster';
COMMENT ON COLUMN delivery.clusters.runtime IS 'Runtime provider, e.g., kubernetes';
COMMENT ON COLUMN delivery.clusters.agent_principal_id IS 'ID of the IAM workload principal of the cluster''s runtime agent; NULL until enrolled; no cross-capability foreign key';
COMMENT ON COLUMN delivery.clusters.agent_capabilities IS 'Cluster-wide facts the agent last reported: its version, the Kubernetes version, watch mode, deployers, and secret store kinds';
COMMENT ON COLUMN delivery.clusters.agent_last_failure IS 'The last refused agent enrolment for this cluster, for the operator: a reason and when; NULL when none';
COMMENT ON COLUMN delivery.clusters.last_reported_at IS 'Timestamp when the cluster''s agent last reported; liveness is the cluster''s, not a target''s';
COMMENT ON COLUMN delivery.clusters.created_at IS 'Timestamp when the cluster was created';
COMMENT ON COLUMN delivery.clusters.updated_at IS 'Timestamp when the cluster was last updated';
COMMENT ON COLUMN delivery.clusters.deleted_at IS 'Timestamp when the cluster was soft-deleted';

-- Lets a Target's foreign key prove it names a Cluster in its own Organization.
CREATE UNIQUE INDEX IF NOT EXISTS idx_clusters_organization ON delivery.clusters(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_clusters_slug ON delivery.clusters(organization_id, slug) WHERE deleted_at IS NULL;
-- One live credential serves one cluster: two clusters cannot name the same agent.
CREATE UNIQUE INDEX IF NOT EXISTS idx_clusters_agent ON delivery.clusters(organization_id, agent_principal_id) WHERE agent_principal_id IS NOT NULL AND deleted_at IS NULL;

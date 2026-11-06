DROP TABLE IF EXISTS iam.agents;
CREATE TABLE IF NOT EXISTS iam.agents (
  principal_id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.agents IS 'A durable agent identity, such as release-agent, that belongs to teams and holds grants; how it runs belongs to orchestration (ADR 0007)';
COMMENT ON COLUMN iam.agents.principal_id IS 'ID of the principal (kind agent) this row details';
COMMENT ON COLUMN iam.agents.organization_id IS 'ID of the organization the agent belongs to';
COMMENT ON COLUMN iam.agents.slug IS 'Slug of the agent, unique among the organization''s agents';

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_organization_slug ON iam.agents(organization_id, slug);

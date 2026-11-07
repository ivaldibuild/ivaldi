CREATE TABLE IF NOT EXISTS orchestration.agent_configs (
  organization_id VARCHAR(30) NOT NULL,
  agent_id VARCHAR(30) PRIMARY KEY,
  system_prompt TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

COMMENT ON TABLE orchestration.agent_configs IS 'How an Agent runs: its system prompt (and later harness, model, and tools); identity stays in IAM (ADR 0007)';
COMMENT ON COLUMN orchestration.agent_configs.organization_id IS 'ID of the organization the agent and its configuration belong to';
COMMENT ON COLUMN orchestration.agent_configs.agent_id IS 'ID of the IAM agent principal this configuration belongs to';
COMMENT ON COLUMN orchestration.agent_configs.system_prompt IS 'Instructions given to the Agent for every Session';

CREATE INDEX IF NOT EXISTS idx_agent_configs_organization_id ON orchestration.agent_configs(organization_id);

CREATE TABLE IF NOT EXISTS iam.agent_sessions (
  principal_id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  session_id VARCHAR(30) NOT NULL,
  work_item_id VARCHAR(30) NOT NULL,
  delegator_principal_id VARCHAR(30) NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, delegator_principal_id) REFERENCES iam.principals (organization_id, id)
);

COMMENT ON TABLE iam.agent_sessions IS 'The identity of one orchestrated Session';
COMMENT ON COLUMN iam.agent_sessions.principal_id IS 'ID of the principal (kind agent_session) this row details';
COMMENT ON COLUMN iam.agent_sessions.organization_id IS 'ID of the organization the session runs in';
COMMENT ON COLUMN iam.agent_sessions.session_id IS 'ID of the orchestration Session; no cross-capability foreign key';
COMMENT ON COLUMN iam.agent_sessions.work_item_id IS 'ID of the Work Item; no cross-capability foreign key';
COMMENT ON COLUMN iam.agent_sessions.delegator_principal_id IS 'ID of the user or service account whose access the session acts with';
COMMENT ON COLUMN iam.agent_sessions.expires_at IS 'Timestamp after which the session identity is no longer valid';
COMMENT ON COLUMN iam.agent_sessions.revoked_at IS 'Timestamp when the session identity was revoked';

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_sessions_session ON iam.agent_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_delegator ON iam.agent_sessions(organization_id, delegator_principal_id);

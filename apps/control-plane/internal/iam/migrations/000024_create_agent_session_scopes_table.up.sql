CREATE TABLE IF NOT EXISTS iam.agent_session_scopes (
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  resource_id VARCHAR(30) NOT NULL,
  PRIMARY KEY (principal_id, resource_id),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, resource_id) REFERENCES iam.resources (organization_id, id)
);

COMMENT ON TABLE iam.agent_session_scopes IS 'Resources an agent session is limited to; effective access is the delegator''s access intersected with these';
COMMENT ON COLUMN iam.agent_session_scopes.organization_id IS 'ID of the organization the session runs in';
COMMENT ON COLUMN iam.agent_session_scopes.principal_id IS 'ID of the agent session principal';
COMMENT ON COLUMN iam.agent_session_scopes.resource_id IS 'ID of a resource the session may act on, including its descendants';

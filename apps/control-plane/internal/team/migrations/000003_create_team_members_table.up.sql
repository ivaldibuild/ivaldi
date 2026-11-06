CREATE TABLE IF NOT EXISTS team.team_members (
  organization_id VARCHAR(30) NOT NULL,
  team_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  role VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, principal_id),
  FOREIGN KEY (organization_id, team_id) REFERENCES team.teams (organization_id, id)
);

COMMENT ON TABLE team.team_members IS 'Members of teams: users now, and agents once IAM models them (ADR 0007)';
COMMENT ON COLUMN team.team_members.organization_id IS 'ID of the organization the team belongs to';
COMMENT ON COLUMN team.team_members.team_id IS 'ID of the team';
COMMENT ON COLUMN team.team_members.principal_id IS 'ID of the member principal; no cross-capability foreign key';
COMMENT ON COLUMN team.team_members.role IS 'Role of the member on the team, e.g., lead, member';
COMMENT ON COLUMN team.team_members.created_at IS 'Timestamp when the principal joined the team';
COMMENT ON COLUMN team.team_members.updated_at IS 'Timestamp when the member''s role last changed';

CREATE INDEX IF NOT EXISTS idx_team_members_principal ON team.team_members(organization_id, principal_id);

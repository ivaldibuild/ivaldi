-- Members are users or agents now (ADR 0007). Rows from before are users;
-- the default only fills them in, and the domain sets the kind from then on.
ALTER TABLE team.team_members ADD COLUMN IF NOT EXISTS principal_kind VARCHAR(50) NOT NULL DEFAULT 'user';
ALTER TABLE team.team_members ALTER COLUMN principal_kind DROP DEFAULT;

COMMENT ON TABLE team.team_members IS 'Members of teams: users and agents (ADR 0007)';
COMMENT ON COLUMN team.team_members.principal_kind IS 'Kind of the member principal in IAM, e.g., user, agent';

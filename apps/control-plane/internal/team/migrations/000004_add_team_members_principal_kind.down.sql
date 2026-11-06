ALTER TABLE team.team_members DROP COLUMN IF EXISTS principal_kind;

COMMENT ON TABLE team.team_members IS 'Members of teams: users now, and agents once IAM models them (ADR 0007)';

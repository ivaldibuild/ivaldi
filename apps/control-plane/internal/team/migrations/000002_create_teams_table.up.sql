CREATE TABLE IF NOT EXISTS team.teams (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL
);

COMMENT ON TABLE team.teams IS 'Teams of humans and agents in an organization; the owners Projects and Applications route work to (ADR 0007)';
COMMENT ON COLUMN team.teams.id IS 'Primary key of the team (tem_ prefixed ULID)';
COMMENT ON COLUMN team.teams.organization_id IS 'ID of the organization the team belongs to; no cross-capability foreign key';
COMMENT ON COLUMN team.teams.slug IS 'Slug of the team, unique among the organization''s active teams';
COMMENT ON COLUMN team.teams.display_name IS 'Display name of the team';
COMMENT ON COLUMN team.teams.description IS 'What the team is responsible for';
COMMENT ON COLUMN team.teams.created_at IS 'Timestamp when the team was created';
COMMENT ON COLUMN team.teams.updated_at IS 'Timestamp when the team was last updated';
COMMENT ON COLUMN team.teams.archived_at IS 'Timestamp when the team was archived; an archived team cannot own new work or change its members';

CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_organization ON team.teams(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_organization_slug ON team.teams(organization_id, slug) WHERE archived_at IS NULL;

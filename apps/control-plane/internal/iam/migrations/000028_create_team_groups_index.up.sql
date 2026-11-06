-- Team groups mirror a Team (ADR 0007): source 'team', with the Team's ID as
-- their external ID. Each Team has at most one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_team ON iam.groups(organization_id, source, external_id) WHERE source = 'team';

COMMENT ON COLUMN iam.groups.source IS 'Who manages the group, e.g., manual, scim, team (mirrors a team, managed only by the team capability)';
COMMENT ON COLUMN iam.groups.external_id IS 'ID of the group in the provisioning system, or of the team a team group mirrors';

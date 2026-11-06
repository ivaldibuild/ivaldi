DROP INDEX IF EXISTS iam.idx_groups_team;

COMMENT ON COLUMN iam.groups.source IS 'Where the group is managed, e.g., manual, scim';
COMMENT ON COLUMN iam.groups.external_id IS 'ID of the group in the provisioning system, if any';

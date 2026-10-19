CREATE TABLE IF NOT EXISTS delivery.channel_rules (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  channel_id VARCHAR(30) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  artifact_id VARCHAR(30) NULL,
  config JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, application_id, channel_id) REFERENCES delivery.channels (organization_id, application_id, id) ON DELETE CASCADE,
  -- Not checked when artifact_id is NULL, so an unscoped rule needs no artifact.
  FOREIGN KEY (organization_id, application_id, artifact_id) REFERENCES delivery.application_artifacts (organization_id, application_id, id)
);

COMMENT ON TABLE delivery.channel_rules IS 'Conditions a release must meet to join a channel; a release must meet every rule';
COMMENT ON COLUMN delivery.channel_rules.id IS 'Primary key of the rule (chr_ prefixed ULID)';
COMMENT ON COLUMN delivery.channel_rules.organization_id IS 'ID of the organization the rule belongs to';
COMMENT ON COLUMN delivery.channel_rules.application_id IS 'ID of the channel''s application; ties the rule''s artifact to the same application';
COMMENT ON COLUMN delivery.channel_rules.channel_id IS 'ID of the channel the rule belongs to';
COMMENT ON COLUMN delivery.channel_rules.kind IS 'Kind of rule, e.g., version, git_ref';
COMMENT ON COLUMN delivery.channel_rules.artifact_id IS 'ID of the artifact the rule is scoped to; NULL means the release version (version) or every build (git_ref)';
COMMENT ON COLUMN delivery.channel_rules.config IS 'Rule parameters, e.g., version range and pre-release pattern, or ref globs; validated by pkg/release';
COMMENT ON COLUMN delivery.channel_rules.created_at IS 'Timestamp when the rule was created';
COMMENT ON COLUMN delivery.channel_rules.updated_at IS 'Timestamp when the rule was last updated';

CREATE INDEX IF NOT EXISTS idx_channel_rules_channel ON delivery.channel_rules(organization_id, channel_id);

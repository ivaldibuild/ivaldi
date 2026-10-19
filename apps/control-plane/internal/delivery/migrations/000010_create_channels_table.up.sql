CREATE TABLE IF NOT EXISTS delivery.channels (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  lifecycle_id VARCHAR(30) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  is_default BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, application_id) REFERENCES delivery.applications (organization_id, id),
  FOREIGN KEY (organization_id, lifecycle_id) REFERENCES delivery.lifecycles (organization_id, id)
);

COMMENT ON TABLE delivery.channels IS 'Release strategies for an application, such as mainline or hotfix; every release belongs to one';
COMMENT ON COLUMN delivery.channels.id IS 'Primary key of the channel (chn_ prefixed ULID)';
COMMENT ON COLUMN delivery.channels.organization_id IS 'ID of the organization the channel belongs to';
COMMENT ON COLUMN delivery.channels.application_id IS 'ID of the application the channel belongs to';
COMMENT ON COLUMN delivery.channels.lifecycle_id IS 'ID of the lifecycle the channel''s releases follow; must be in the application''s workspace';
COMMENT ON COLUMN delivery.channels.slug IS 'Slug of the channel, unique within the application';
COMMENT ON COLUMN delivery.channels.display_name IS 'Display name of the channel';
COMMENT ON COLUMN delivery.channels.description IS 'Description of when the channel is used';
COMMENT ON COLUMN delivery.channels.is_default IS 'Whether this is the application''s default channel; exactly one per application';
COMMENT ON COLUMN delivery.channels.created_at IS 'Timestamp when the channel was created';
COMMENT ON COLUMN delivery.channels.updated_at IS 'Timestamp when the channel was last updated';
COMMENT ON COLUMN delivery.channels.archived_at IS 'Timestamp when the channel was archived; the default channel cannot be archived';

CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_organization ON delivery.channels(organization_id, id);
-- Lets releases and rules check that a channel belongs to their application.
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_application ON delivery.channels(organization_id, application_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_application_slug ON delivery.channels(organization_id, application_id, slug) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_default ON delivery.channels(organization_id, application_id) WHERE is_default;
CREATE INDEX IF NOT EXISTS idx_channels_lifecycle ON delivery.channels(organization_id, lifecycle_id);

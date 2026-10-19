CREATE TABLE IF NOT EXISTS delivery.releases (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  channel_id VARCHAR(30) NOT NULL,
  version VARCHAR(255) NOT NULL,
  status VARCHAR(50) NOT NULL,
  source VARCHAR(50) NOT NULL,
  created_by_principal_id VARCHAR(30) NULL,
  publish_decision_id VARCHAR(30) NULL,
  published_at TIMESTAMPTZ NULL,
  withdrawn_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Including application_id ties the release to a channel of its own application.
  FOREIGN KEY (organization_id, application_id, channel_id) REFERENCES delivery.channels (organization_id, application_id, id)
);

COMMENT ON TABLE delivery.releases IS 'Versions of an application in one channel; channel and builds are fixed once published';
COMMENT ON COLUMN delivery.releases.id IS 'Primary key of the release (rel_ prefixed ULID)';
COMMENT ON COLUMN delivery.releases.organization_id IS 'ID of the organization the release belongs to';
COMMENT ON COLUMN delivery.releases.application_id IS 'ID of the application the release versions';
COMMENT ON COLUMN delivery.releases.channel_id IS 'ID of the channel the release belongs to; cannot change once published';
COMMENT ON COLUMN delivery.releases.version IS 'Version of the release, unique within the application; parsed and compared by pkg/release';
COMMENT ON COLUMN delivery.releases.status IS 'Status of the release, e.g., draft, published, withdrawn';
COMMENT ON COLUMN delivery.releases.source IS 'How the release was made, e.g., created, observed';
COMMENT ON COLUMN delivery.releases.created_by_principal_id IS 'ID of the principal that created the release; NULL for observed releases; no cross-capability foreign key';
COMMENT ON COLUMN delivery.releases.publish_decision_id IS 'ID of the authorizer decision, including any human approval, that allowed publishing; no cross-capability foreign key';
COMMENT ON COLUMN delivery.releases.published_at IS 'Timestamp when the release was published';
COMMENT ON COLUMN delivery.releases.withdrawn_at IS 'Timestamp when the release was withdrawn to stop new deployments of it';
COMMENT ON COLUMN delivery.releases.created_at IS 'Timestamp when the release was created';
COMMENT ON COLUMN delivery.releases.updated_at IS 'Timestamp when the release was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_organization ON delivery.releases(organization_id, id);
-- Lets deployments, release builds, and placements check that a release belongs to their application.
CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_application ON delivery.releases(organization_id, application_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_releases_version ON delivery.releases(organization_id, application_id, version);
CREATE INDEX IF NOT EXISTS idx_releases_channel ON delivery.releases(organization_id, channel_id);

ALTER TABLE delivery.application_targets
  ADD CONSTRAINT fk_application_targets_current_release
  FOREIGN KEY (organization_id, application_id, current_release_id) REFERENCES delivery.releases (organization_id, application_id, id);

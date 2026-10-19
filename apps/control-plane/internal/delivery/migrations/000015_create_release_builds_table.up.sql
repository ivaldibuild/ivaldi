CREATE TABLE IF NOT EXISTS delivery.release_builds (
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  release_id VARCHAR(30) NOT NULL,
  artifact_id VARCHAR(30) NOT NULL,
  build_id VARCHAR(30) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (release_id, artifact_id),
  FOREIGN KEY (organization_id, application_id, release_id) REFERENCES delivery.releases (organization_id, application_id, id),
  FOREIGN KEY (organization_id, application_id, artifact_id) REFERENCES delivery.application_artifacts (organization_id, application_id, id),
  FOREIGN KEY (organization_id, build_id) REFERENCES delivery.builds (organization_id, id)
);

COMMENT ON TABLE delivery.release_builds IS 'The build for each artifact in a release; fixed once the release is published';
COMMENT ON COLUMN delivery.release_builds.organization_id IS 'ID of the organization the release belongs to';
COMMENT ON COLUMN delivery.release_builds.application_id IS 'ID of the release''s application; ties the artifact to the same application';
COMMENT ON COLUMN delivery.release_builds.release_id IS 'ID of the release';
COMMENT ON COLUMN delivery.release_builds.artifact_id IS 'ID of the application artifact this build fills';
COMMENT ON COLUMN delivery.release_builds.build_id IS 'ID of the build; its artifact_name must match the artifact''s';
COMMENT ON COLUMN delivery.release_builds.created_at IS 'Timestamp when the build was added to the release';

CREATE INDEX IF NOT EXISTS idx_release_builds_build ON delivery.release_builds(organization_id, build_id);

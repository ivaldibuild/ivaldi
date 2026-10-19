CREATE TABLE IF NOT EXISTS delivery.application_artifacts (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  application_id VARCHAR(30) NOT NULL,
  name VARCHAR(100) NOT NULL,
  artifact_kind VARCHAR(50) NOT NULL,
  artifact_name VARCHAR(512) NOT NULL,
  registry_id VARCHAR(30) NULL,
  required BOOLEAN NOT NULL DEFAULT true,
  source VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at TIMESTAMPTZ NULL,
  FOREIGN KEY (organization_id, application_id) REFERENCES delivery.applications (organization_id, id),
  FOREIGN KEY (organization_id, registry_id) REFERENCES delivery.registries (organization_id, id)
);

COMMENT ON TABLE delivery.application_artifacts IS 'The artifacts an application is made of, such as images and a Helm chart; a release has one build per artifact';
COMMENT ON COLUMN delivery.application_artifacts.id IS 'Primary key of the artifact (art_ prefixed ULID)';
COMMENT ON COLUMN delivery.application_artifacts.organization_id IS 'ID of the organization the artifact belongs to';
COMMENT ON COLUMN delivery.application_artifacts.application_id IS 'ID of the application the artifact is part of';
COMMENT ON COLUMN delivery.application_artifacts.name IS 'Name of the artifact within the application, e.g., api, migrate, chart';
COMMENT ON COLUMN delivery.application_artifacts.artifact_kind IS 'Kind of artifact, e.g., container_image, helm_chart, archive';
COMMENT ON COLUMN delivery.application_artifacts.artifact_name IS 'Name builds are published under, e.g., ghcr.io/acme/api; used to match builds and running images';
COMMENT ON COLUMN delivery.application_artifacts.registry_id IS 'ID of the registry used to look the artifact up, if any';
COMMENT ON COLUMN delivery.application_artifacts.required IS 'Whether every published release must include a build of this artifact';
COMMENT ON COLUMN delivery.application_artifacts.source IS 'How the artifact became known, e.g., declared, observed';
COMMENT ON COLUMN delivery.application_artifacts.created_at IS 'Timestamp when the artifact was created';
COMMENT ON COLUMN delivery.application_artifacts.updated_at IS 'Timestamp when the artifact was last updated';
COMMENT ON COLUMN delivery.application_artifacts.archived_at IS 'Timestamp when the artifact was archived';

CREATE UNIQUE INDEX IF NOT EXISTS idx_application_artifacts_organization ON delivery.application_artifacts(organization_id, id);
-- Lets rules and release builds check that an artifact belongs to their application.
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_artifacts_application ON delivery.application_artifacts(organization_id, application_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_artifacts_name ON delivery.application_artifacts(organization_id, application_id, name) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_application_artifacts_artifact_name ON delivery.application_artifacts(organization_id, artifact_name);

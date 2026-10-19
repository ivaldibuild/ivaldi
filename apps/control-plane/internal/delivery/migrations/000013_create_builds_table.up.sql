CREATE TABLE IF NOT EXISTS delivery.builds (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  artifact_kind VARCHAR(50) NOT NULL,
  artifact_name VARCHAR(512) NOT NULL,
  digest VARCHAR(255) NOT NULL,
  version VARCHAR(255) NULL,
  provenance VARCHAR(50) NOT NULL,
  source_id VARCHAR(30) NULL,
  commit_sha VARCHAR(64) NULL,
  source_ref TEXT NULL,
  ci_provider VARCHAR(50) NULL,
  ci_run_ref TEXT NULL,
  reported_tags TEXT[] NOT NULL DEFAULT '{}',
  built_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE delivery.builds IS 'One version of one artifact, identified by its digest; immutable except for the provenance upgrade of an observed build';
COMMENT ON COLUMN delivery.builds.id IS 'Primary key of the build (bld_ prefixed ULID)';
COMMENT ON COLUMN delivery.builds.organization_id IS 'ID of the organization the build belongs to; no cross-capability foreign key';
COMMENT ON COLUMN delivery.builds.artifact_kind IS 'Kind of artifact, e.g., container_image, helm_chart, archive';
COMMENT ON COLUMN delivery.builds.artifact_name IS 'Name the build is published under, e.g., ghcr.io/acme/api';
COMMENT ON COLUMN delivery.builds.digest IS 'Content digest, e.g., sha256:<hex>; the build''s identity, never a tag';
COMMENT ON COLUMN delivery.builds.version IS 'Version reported by CI, if any; a label, not the identity';
COMMENT ON COLUMN delivery.builds.provenance IS 'Where the build is known from, e.g., ci_reported, observed';
COMMENT ON COLUMN delivery.builds.source_id IS 'ID of the source the build was built from; no cross-capability foreign key';
COMMENT ON COLUMN delivery.builds.commit_sha IS 'Commit the build was built from; never changes once reported by CI';
COMMENT ON COLUMN delivery.builds.source_ref IS 'Git ref the build was built from, e.g., refs/heads/main';
COMMENT ON COLUMN delivery.builds.ci_provider IS 'CI system that reported the build, e.g., github_actions';
COMMENT ON COLUMN delivery.builds.ci_run_ref IS 'Reference to the CI run that produced the build';
COMMENT ON COLUMN delivery.builds.reported_tags IS 'Tags seen with the digest; hints only, because tags can move';
COMMENT ON COLUMN delivery.builds.built_at IS 'Timestamp when CI built the artifact';
COMMENT ON COLUMN delivery.builds.created_at IS 'Timestamp when the build was first recorded';
COMMENT ON COLUMN delivery.builds.updated_at IS 'Timestamp when the build was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_organization ON delivery.builds(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_builds_digest ON delivery.builds(organization_id, artifact_name, digest);
CREATE INDEX IF NOT EXISTS idx_builds_commit ON delivery.builds(organization_id, source_id, commit_sha) WHERE commit_sha IS NOT NULL;

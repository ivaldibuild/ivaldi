CREATE TABLE IF NOT EXISTS provenance.change_commits (
  organization_id VARCHAR(30) NOT NULL,
  source_id VARCHAR(30) NOT NULL,
  commit_sha VARCHAR(64) NOT NULL,
  change_id VARCHAR(30) NOT NULL,
  PRIMARY KEY (organization_id, source_id, commit_sha),
  FOREIGN KEY (organization_id, change_id) REFERENCES provenance.changes (organization_id, id)
);

COMMENT ON TABLE provenance.change_commits IS 'The commits a Change contains; a commit belongs to at most one Change in its Source, so release notes map it once';
COMMENT ON COLUMN provenance.change_commits.organization_id IS 'ID of the organization the change belongs to';
COMMENT ON COLUMN provenance.change_commits.source_id IS 'ID of the source the commit belongs to';
COMMENT ON COLUMN provenance.change_commits.commit_sha IS 'Commit SHA, full or abbreviated as reported';
COMMENT ON COLUMN provenance.change_commits.change_id IS 'ID of the Change the commit belongs to';

CREATE INDEX IF NOT EXISTS idx_change_commits_change ON provenance.change_commits(organization_id, change_id);

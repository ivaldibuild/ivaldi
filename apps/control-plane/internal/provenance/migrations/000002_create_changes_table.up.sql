CREATE TABLE IF NOT EXISTS provenance.changes (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  source_id VARCHAR(30) NOT NULL,
  session_id VARCHAR(30) NULL,
  work_item_id VARCHAR(30) NULL,
  title VARCHAR(512) NOT NULL,
  external_ref TEXT NULL,
  created_by_principal_id VARCHAR(30) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE provenance.changes IS 'A set of commits in a Source and the Session and Work Item that produced them; the link release notes derive Changes and Work Items through (ADR 0006)';
COMMENT ON COLUMN provenance.changes.id IS 'Primary key of the change (chg_ prefixed ULID)';
COMMENT ON COLUMN provenance.changes.organization_id IS 'ID of the organization the change belongs to; no cross-capability foreign key';
COMMENT ON COLUMN provenance.changes.source_id IS 'ID of the source the commits belong to; no cross-capability foreign key';
COMMENT ON COLUMN provenance.changes.session_id IS 'ID of the Session that produced the change, if known; no cross-capability foreign key';
COMMENT ON COLUMN provenance.changes.work_item_id IS 'ID of the Work Item the change serves, if known; no cross-capability foreign key';
COMMENT ON COLUMN provenance.changes.title IS 'Short description of the change';
COMMENT ON COLUMN provenance.changes.external_ref IS 'Provider reference such as a pull request URL, if any';
COMMENT ON COLUMN provenance.changes.created_by_principal_id IS 'ID of the principal that recorded the change; no cross-capability foreign key';
COMMENT ON COLUMN provenance.changes.created_at IS 'Timestamp when the change was recorded';
COMMENT ON COLUMN provenance.changes.updated_at IS 'Timestamp when the change was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_changes_organization ON provenance.changes(organization_id, id);
CREATE INDEX IF NOT EXISTS idx_changes_source ON provenance.changes(organization_id, source_id);
CREATE INDEX IF NOT EXISTS idx_changes_session ON provenance.changes(organization_id, session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_changes_work_item ON provenance.changes(organization_id, work_item_id) WHERE work_item_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS iam.principals (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  kind VARCHAR(50) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  status VARCHAR(50) NOT NULL,
  metadata JSONB NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.principals IS 'Table storing principal details';
COMMENT ON COLUMN iam.principals.id IS 'Primary key of the principal';
COMMENT ON COLUMN iam.principals.organization_id IS 'ID of the organization the principal belongs to';
COMMENT ON COLUMN iam.principals.kind IS 'Kind of the principal, e.g., user, service account, etc.';
COMMENT ON COLUMN iam.principals.display_name IS 'Display name of the principal';
COMMENT ON COLUMN iam.principals.status IS 'Status of the principal, e.g., active, inactive';
COMMENT ON COLUMN iam.principals.metadata IS 'Additional metadata for the principal in JSONB format';
COMMENT ON COLUMN iam.principals.created_at IS 'Timestamp when the principal was created';
COMMENT ON COLUMN iam.principals.updated_at IS 'Timestamp when the principal was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_principals_organization ON iam.principals(organization_id, id);

CREATE TABLE IF NOT EXISTS iam.organizations (
  id varchar(30) PRIMARY KEY,
  slug varchar(255) NOT NULL,
  name varchar(255) NOT NULL,
  display_name varchar(255) NOT NULL,
  logo_url text NULL,
  status varchar(50) NOT NULL,
  metadata jsonb NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.organizations IS 'Table storing organization details';
COMMENT ON COLUMN iam.organizations.id IS 'Primary key of the organization';
COMMENT ON COLUMN iam.organizations.slug IS 'Unique slug for the organization';
COMMENT ON COLUMN iam.organizations.name IS 'Name of the organization';
COMMENT ON COLUMN iam.organizations.display_name IS 'Display name of the organization';
COMMENT ON COLUMN iam.organizations.logo_url IS 'URL of the organization logo';
COMMENT ON COLUMN iam.organizations.status IS 'Status of the organization';
COMMENT ON COLUMN iam.organizations.metadata IS 'Additional metadata for the organization';
COMMENT ON COLUMN iam.organizations.created_at IS 'Timestamp when the organization was created';
COMMENT ON COLUMN iam.organizations.updated_at IS 'Timestamp when the organization was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_slug ON iam.organizations(slug);



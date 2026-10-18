CREATE TABLE IF NOT EXISTS iam.external_identities (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  principal_id VARCHAR(30) NOT NULL,
  identity_provider_id VARCHAR(30) NOT NULL,
  subject VARCHAR(512) NOT NULL,
  last_login_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, principal_id) REFERENCES iam.principals (organization_id, id),
  FOREIGN KEY (organization_id, identity_provider_id) REFERENCES iam.identity_providers (organization_id, id)
);

COMMENT ON TABLE iam.external_identities IS 'Links an organization membership to an identity-provider subject';
COMMENT ON COLUMN iam.external_identities.id IS 'Primary key of the external identity (xid_ prefixed ULID)';
COMMENT ON COLUMN iam.external_identities.organization_id IS 'ID of the organization the identity provider belongs to';
COMMENT ON COLUMN iam.external_identities.principal_id IS 'ID of the user principal; deliberately not the account, so an organization''s provider can only sign in to that organization';
COMMENT ON COLUMN iam.external_identities.identity_provider_id IS 'ID of the identity provider asserting the subject';
COMMENT ON COLUMN iam.external_identities.subject IS 'OIDC sub or SAML NameID';
COMMENT ON COLUMN iam.external_identities.last_login_at IS 'Timestamp of the last sign-in through this identity';
COMMENT ON COLUMN iam.external_identities.created_at IS 'Timestamp when the identity was linked';

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_identities_subject ON iam.external_identities(identity_provider_id, subject);
CREATE INDEX IF NOT EXISTS idx_external_identities_principal ON iam.external_identities(organization_id, principal_id);

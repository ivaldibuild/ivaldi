CREATE TABLE IF NOT EXISTS iam.identity_providers (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  protocol VARCHAR(50) NOT NULL,
  name VARCHAR(255) NOT NULL,
  config JSONB NOT NULL DEFAULT '{}',
  secret_ref TEXT NULL,
  jit_provisioning BOOLEAN NOT NULL DEFAULT false,
  status VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE iam.identity_providers IS 'Per-organization identity provider configuration';
COMMENT ON COLUMN iam.identity_providers.id IS 'Primary key of the identity provider (idp_ prefixed ULID)';
COMMENT ON COLUMN iam.identity_providers.organization_id IS 'ID of the organization the provider belongs to';
COMMENT ON COLUMN iam.identity_providers.protocol IS 'Protocol, e.g., oidc, saml, builtin';
COMMENT ON COLUMN iam.identity_providers.name IS 'Name of the identity provider';
COMMENT ON COLUMN iam.identity_providers.config IS 'Non-secret settings such as issuer, client ID, metadata URL';
COMMENT ON COLUMN iam.identity_providers.secret_ref IS 'Credential reference for provider secrets (ADR 0004); never a raw secret';
COMMENT ON COLUMN iam.identity_providers.jit_provisioning IS 'Whether a first sign-in creates the organization membership';
COMMENT ON COLUMN iam.identity_providers.status IS 'Status of the identity provider, e.g., active, disabled';
COMMENT ON COLUMN iam.identity_providers.created_at IS 'Timestamp when the identity provider was created';
COMMENT ON COLUMN iam.identity_providers.updated_at IS 'Timestamp when the identity provider was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_providers_organization ON iam.identity_providers(organization_id, id);

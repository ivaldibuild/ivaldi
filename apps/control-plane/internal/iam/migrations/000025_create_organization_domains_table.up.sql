CREATE TABLE IF NOT EXISTS iam.organization_domains (
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  domain VARCHAR(253) NOT NULL,
  verification_token_hash BYTEA NULL,
  verified_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, domain)
);

COMMENT ON TABLE iam.organization_domains IS 'Email domains an organization has claimed; used to route sign-in and to link memberships to existing accounts';
COMMENT ON COLUMN iam.organization_domains.organization_id IS 'ID of the organization claiming the domain';
COMMENT ON COLUMN iam.organization_domains.domain IS 'Domain name, stored lowercase';
COMMENT ON COLUMN iam.organization_domains.verification_token_hash IS 'SHA-256 hash of the DNS TXT verification token; never the raw token';
COMMENT ON COLUMN iam.organization_domains.verified_at IS 'Timestamp when ownership was verified; unverified domains grant nothing';
COMMENT ON COLUMN iam.organization_domains.created_at IS 'Timestamp when the domain was claimed';

-- Several organizations may claim a domain, but only one can hold it verified.
CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_domains_verified ON iam.organization_domains(lower(domain)) WHERE verified_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS iam.signing_keys (
  kid VARCHAR(255) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL REFERENCES iam.organizations (id),
  algorithm VARCHAR(50) NOT NULL,
  public_jwk JSONB NOT NULL,
  private_key_ref TEXT NOT NULL,
  status VARCHAR(50) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  not_after TIMESTAMPTZ NULL
);

COMMENT ON TABLE iam.signing_keys IS 'Per-organization token signing keys, published through JWKS';
COMMENT ON COLUMN iam.signing_keys.kid IS 'JWKS key ID; opaque, not a prefixed ULID';
COMMENT ON COLUMN iam.signing_keys.organization_id IS 'ID of the organization the key signs for';
COMMENT ON COLUMN iam.signing_keys.algorithm IS 'JWS algorithm, e.g., EdDSA, ES256';
COMMENT ON COLUMN iam.signing_keys.public_jwk IS 'Public key as a JWK';
COMMENT ON COLUMN iam.signing_keys.private_key_ref IS 'Credential or KMS reference to the private key (ADR 0004); never the key itself';
COMMENT ON COLUMN iam.signing_keys.status IS 'Rotation status, e.g., active, retiring, retired';
COMMENT ON COLUMN iam.signing_keys.created_at IS 'Timestamp when the key was created';
COMMENT ON COLUMN iam.signing_keys.not_after IS 'Timestamp after which verifiers stop accepting the key';

CREATE INDEX IF NOT EXISTS idx_signing_keys_organization ON iam.signing_keys(organization_id);

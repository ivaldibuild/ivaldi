ALTER TABLE iam.users ADD COLUMN IF NOT EXISTS last_signed_in_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN iam.users.last_signed_in_at IS 'Timestamp when the user last signed in to the organization; NULL until they first do';

-- Some events belong to no Organization, such as linking a platform sign-in
-- to an Account or registering a first-party OAuth client.
ALTER TABLE outbox.events ALTER COLUMN organization_id DROP NOT NULL;

COMMENT ON COLUMN outbox.events.organization_id IS 'Organization the raising object belongs to, or NULL for a platform-scoped object such as an account; no cross-capability foreign key';

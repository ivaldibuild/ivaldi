CREATE TABLE IF NOT EXISTS notifications.notifications (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  recipient_principal_id VARCHAR(30) NOT NULL,
  kind VARCHAR(50) NOT NULL,
  subject_type VARCHAR(100) NOT NULL,
  subject_id VARCHAR(300) NOT NULL,
  event_id VARCHAR(100) NOT NULL,
  title VARCHAR(255) NOT NULL,
  body VARCHAR(4000) NOT NULL,
  link VARCHAR(500) NOT NULL,
  owner_team_id VARCHAR(30) NULL,
  read_at TIMESTAMPTZ NULL,
  resolved_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_notifications_kind CHECK (kind IN ('approval_requested', 'approval_decided'))
);

COMMENT ON TABLE notifications.notifications IS 'Messages to one person about work: what it is about, what they read, and whether it still waits. Contact records: no domain events or audit records';
COMMENT ON COLUMN notifications.notifications.id IS 'Primary key of the notification (ntf_ prefixed ULID)';
COMMENT ON COLUMN notifications.notifications.organization_id IS 'ID of the organization; no cross-capability foreign key';
COMMENT ON COLUMN notifications.notifications.recipient_principal_id IS 'ID of the user it is for; no cross-capability foreign key';
COMMENT ON COLUMN notifications.notifications.kind IS 'approval_requested asks someone to decide a request; approval_decided tells the requester''s side how it was decided';
COMMENT ON COLUMN notifications.notifications.subject_type IS 'Type of what it is about, such as policy.approval_request';
COMMENT ON COLUMN notifications.notifications.subject_id IS 'ID of what it is about, in the capability that owns it; no cross-capability foreign key';
COMMENT ON COLUMN notifications.notifications.event_id IS 'ID of the outbox event that caused it; with the recipient, unique, so a redelivered event adds nothing';
COMMENT ON COLUMN notifications.notifications.title IS 'One line naming what it is about, copied when it was made';
COMMENT ON COLUMN notifications.notifications.body IS 'What the recipient reads, copied when it was made';
COMMENT ON COLUMN notifications.notifications.link IS 'Path in the web app it opens, such as /approvals/apr_…';
COMMENT ON COLUMN notifications.notifications.owner_team_id IS 'ID of the team it was routed through, if any; no cross-capability foreign key';
COMMENT ON COLUMN notifications.notifications.read_at IS 'Timestamp when the recipient read it; NULL while unread';
COMMENT ON COLUMN notifications.notifications.resolved_at IS 'Timestamp when what it asked for stopped waiting; NULL while it still waits or asks for nothing';
COMMENT ON COLUMN notifications.notifications.created_at IS 'Timestamp when it was made';

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_event_recipient
  ON notifications.notifications(organization_id, event_id, recipient_principal_id);
CREATE INDEX IF NOT EXISTS idx_notifications_inbox
  ON notifications.notifications(organization_id, recipient_principal_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_notifications_subject
  ON notifications.notifications(organization_id, subject_type, subject_id) WHERE resolved_at IS NULL;

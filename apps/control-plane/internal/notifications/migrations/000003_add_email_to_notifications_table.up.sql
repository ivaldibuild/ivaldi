ALTER TABLE notifications.notifications
  ADD COLUMN IF NOT EXISTS email_status VARCHAR(20) NOT NULL DEFAULT 'not_sent',
  ADD COLUMN IF NOT EXISTS emailed_at TIMESTAMPTZ NULL;

ALTER TABLE notifications.notifications
  ADD CONSTRAINT chk_notifications_email_status CHECK (email_status IN ('pending', 'sent', 'not_sent')),
  ADD CONSTRAINT chk_notifications_emailed CHECK ((email_status = 'sent') = (emailed_at IS NOT NULL));

COMMENT ON COLUMN notifications.notifications.email_status IS 'pending while an email is owed, sent once the SMTP server accepted it, not_sent when email is off or the recipient has no address';
COMMENT ON COLUMN notifications.notifications.emailed_at IS 'Timestamp when the SMTP server accepted its email; NULL unless sent';

-- The emails an event still owes are found by event.
CREATE INDEX IF NOT EXISTS idx_notifications_pending_email
  ON notifications.notifications(organization_id, event_id) WHERE email_status = 'pending';

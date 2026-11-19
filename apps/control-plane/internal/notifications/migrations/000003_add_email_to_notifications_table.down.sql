DROP INDEX IF EXISTS notifications.idx_notifications_pending_email;
ALTER TABLE notifications.notifications
  DROP CONSTRAINT IF EXISTS chk_notifications_emailed,
  DROP CONSTRAINT IF EXISTS chk_notifications_email_status,
  DROP COLUMN IF EXISTS emailed_at,
  DROP COLUMN IF EXISTS email_status;

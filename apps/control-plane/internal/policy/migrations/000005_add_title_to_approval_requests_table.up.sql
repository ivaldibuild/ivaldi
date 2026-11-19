ALTER TABLE policy.approval_requests
  ADD COLUMN IF NOT EXISTS title VARCHAR(255) NULL;

-- Requests opened before titles existed are named by their subject, cut to
-- fit the column.
UPDATE policy.approval_requests
SET title = CASE WHEN char_length(subject_type || ' ' || subject_id) > 255
  THEN left(subject_type || ' ' || subject_id, 254) || '…'
  ELSE subject_type || ' ' || subject_id END
WHERE title IS NULL;

-- The default is for the previous release's binary, which writes no title,
-- during an upgrade or after a rollback. A later migration drops it once no
-- supported binary omits the title.
ALTER TABLE policy.approval_requests
  ALTER COLUMN title SET DEFAULT 'Approval request',
  ALTER COLUMN title SET NOT NULL;

COMMENT ON COLUMN policy.approval_requests.title IS 'One line naming what waits, written by the subject''s capability when the request was opened, such as "Deploy api 1.4.0 to Production / eu-west"';

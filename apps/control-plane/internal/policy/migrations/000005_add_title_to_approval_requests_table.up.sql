ALTER TABLE policy.approval_requests
  ADD COLUMN IF NOT EXISTS title VARCHAR(255) NULL;

-- Requests opened before titles existed are named by their subject.
UPDATE policy.approval_requests SET title = subject_type || ' ' || subject_id WHERE title IS NULL;

ALTER TABLE policy.approval_requests
  ALTER COLUMN title SET NOT NULL;

COMMENT ON COLUMN policy.approval_requests.title IS 'One line naming what waits, written by the subject''s capability when the request was opened, such as "Deploy api 1.4.0 to Production / eu-west"';

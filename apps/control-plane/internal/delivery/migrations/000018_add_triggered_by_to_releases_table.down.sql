ALTER TABLE delivery.releases
  DROP COLUMN IF EXISTS triggered_by_email,
  DROP COLUMN IF EXISTS triggered_by;

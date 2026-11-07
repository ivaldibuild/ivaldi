ALTER TABLE delivery.releases
  ADD COLUMN triggered_by VARCHAR(255) NULL,
  ADD COLUMN triggered_by_email VARCHAR(320) NULL;

COMMENT ON COLUMN delivery.releases.triggered_by IS 'Who triggered the release, such as the CI user who ran the pipeline; reported by the caller, not verified';
COMMENT ON COLUMN delivery.releases.triggered_by_email IS 'Email address of whoever triggered the release; reported by the caller, not verified';

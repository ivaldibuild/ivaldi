ALTER TABLE IF EXISTS delivery.application_targets DROP CONSTRAINT IF EXISTS fk_application_targets_current_release;
DROP TABLE IF EXISTS delivery.releases;

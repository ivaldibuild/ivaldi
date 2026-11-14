-- Apply config now names the chart's overlay values files, not references to
-- whole values documents. Creating a Placement validates the config, so the
-- retired keys are removed here rather than left to fail on load.
UPDATE delivery.application_targets
SET apply_config = apply_config - 'values_refs'
WHERE apply_config ? 'values_refs';

COMMENT ON COLUMN delivery.application_targets.apply_config IS 'How Ivaldi applies a Release when executing: the artifact that holds the deployment definition and the chart''s overlay values files; used only for executed deployments';

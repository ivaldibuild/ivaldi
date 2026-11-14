ALTER TABLE delivery.targets
  ADD COLUMN capability_reason VARCHAR(512) NOT NULL DEFAULT '';

COMMENT ON COLUMN delivery.targets.capability_reason IS 'Why the cluster''s agent last reported it cannot observe or execute in the target''s namespace, such as an RBAC denial; empty when nothing is missing';

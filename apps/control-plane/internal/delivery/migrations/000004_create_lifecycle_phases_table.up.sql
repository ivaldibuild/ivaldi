CREATE TABLE IF NOT EXISTS delivery.lifecycle_phases (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  lifecycle_id VARCHAR(30) NOT NULL,
  position INTEGER NOT NULL,
  name VARCHAR(255) NOT NULL,
  optional BOOLEAN NOT NULL DEFAULT false,
  min_environments INTEGER NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_lifecycle_phases_position CHECK (position >= 0),
  CONSTRAINT chk_lifecycle_phases_min_environments CHECK (min_environments IS NULL OR min_environments >= 1),
  -- Deferred so phases can be reordered within one transaction.
  CONSTRAINT uq_lifecycle_phases_position UNIQUE (lifecycle_id, position) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (organization_id, lifecycle_id) REFERENCES delivery.lifecycles (organization_id, id)
);

COMMENT ON TABLE delivery.lifecycle_phases IS 'One ordered step in a lifecycle; a phase opens for a release once earlier required phases are met';
COMMENT ON COLUMN delivery.lifecycle_phases.id IS 'Primary key of the phase (lfp_ prefixed ULID)';
COMMENT ON COLUMN delivery.lifecycle_phases.organization_id IS 'ID of the organization the phase belongs to';
COMMENT ON COLUMN delivery.lifecycle_phases.lifecycle_id IS 'ID of the lifecycle the phase is part of';
COMMENT ON COLUMN delivery.lifecycle_phases.position IS 'Order of the phase within its lifecycle, starting at 0; unique within the lifecycle';
COMMENT ON COLUMN delivery.lifecycle_phases.name IS 'Name of the phase';
COMMENT ON COLUMN delivery.lifecycle_phases.optional IS 'Whether a release may skip this phase';
COMMENT ON COLUMN delivery.lifecycle_phases.min_environments IS 'How many of the phase''s environments need a successful deployment before the next phase opens; NULL means all';
COMMENT ON COLUMN delivery.lifecycle_phases.created_at IS 'Timestamp when the phase was created';
COMMENT ON COLUMN delivery.lifecycle_phases.updated_at IS 'Timestamp when the phase was last updated';

CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycle_phases_organization ON delivery.lifecycle_phases(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycle_phases_lifecycle ON delivery.lifecycle_phases(organization_id, lifecycle_id, id);

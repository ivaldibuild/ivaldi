CREATE TABLE IF NOT EXISTS delivery.variable_values (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  variable_id VARCHAR(30) NOT NULL,
  text_value TEXT NULL,
  environment_ids TEXT[] NOT NULL DEFAULT '{}',
  target_ids TEXT[] NOT NULL DEFAULT '{}',
  channel_ids TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (organization_id, variable_id) REFERENCES delivery.variables (organization_id, id)
);

COMMENT ON TABLE delivery.variable_values IS 'One value of a variable and the deployments it applies to; the most specific matching value wins';
COMMENT ON COLUMN delivery.variable_values.id IS 'Primary key of the value (vvl_ prefixed ULID)';
COMMENT ON COLUMN delivery.variable_values.organization_id IS 'ID of the organization the value belongs to';
COMMENT ON COLUMN delivery.variable_values.variable_id IS 'ID of the variable the value belongs to';
COMMENT ON COLUMN delivery.variable_values.text_value IS 'The value of a text variable, which may contain #{Name} bindings';
COMMENT ON COLUMN delivery.variable_values.environment_ids IS 'IDs of the environments the value is scoped to; empty means any';
COMMENT ON COLUMN delivery.variable_values.target_ids IS 'IDs of the targets the value is scoped to; empty means any';
COMMENT ON COLUMN delivery.variable_values.channel_ids IS 'IDs of the channels the value is scoped to; empty means any, and always empty for a project''s variable';
COMMENT ON COLUMN delivery.variable_values.created_at IS 'Timestamp when the value was created';
COMMENT ON COLUMN delivery.variable_values.updated_at IS 'Timestamp when the value was last updated';

CREATE INDEX IF NOT EXISTS idx_variable_values_variable ON delivery.variable_values(organization_id, variable_id);
-- Find the values that name an environment, target, or channel being archived.
CREATE INDEX IF NOT EXISTS idx_variable_values_environments ON delivery.variable_values USING GIN (environment_ids);
CREATE INDEX IF NOT EXISTS idx_variable_values_targets ON delivery.variable_values USING GIN (target_ids);
CREATE INDEX IF NOT EXISTS idx_variable_values_channels ON delivery.variable_values USING GIN (channel_ids);

DROP INDEX IF EXISTS delivery.idx_variable_values_store;
ALTER TABLE delivery.variable_values DROP CONSTRAINT IF EXISTS chk_variable_values_content;
ALTER TABLE delivery.variable_values
  DROP COLUMN IF EXISTS store_id,
  DROP COLUMN IF EXISTS path,
  DROP COLUMN IF EXISTS field,
  DROP COLUMN IF EXISTS version;

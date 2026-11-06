CREATE TABLE IF NOT EXISTS policy.setting_locks (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  workspace_id VARCHAR(30) NULL,
  key VARCHAR(255) NOT NULL,
  locked_by_principal_id VARCHAR(30) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Keys are the workspace capability's setting keys, namespaced by the capability that defines them.
  CONSTRAINT chk_setting_locks_key_format CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$')
);

COMMENT ON TABLE policy.setting_locks IS 'Setting locks: policy rules that fix a setting key at an organization, for all its workspaces and projects, or at a workspace, for its projects. Removing a lock deletes its row; the audit record and the removed event keep its history';
COMMENT ON COLUMN policy.setting_locks.id IS 'Primary key of the lock (slk_ prefixed ULID)';
COMMENT ON COLUMN policy.setting_locks.organization_id IS 'ID of the organization the lock belongs to; on its own, the lock applies to the whole organization; no cross-capability foreign key';
COMMENT ON COLUMN policy.setting_locks.workspace_id IS 'ID of the workspace the lock applies to, for its projects; NULL for an organization lock; no cross-capability foreign key';
COMMENT ON COLUMN policy.setting_locks.key IS 'Setting key the lock fixes, such as delivery.default_lifecycle; defined by the capability that owns the setting';
COMMENT ON COLUMN policy.setting_locks.locked_by_principal_id IS 'ID of the principal that added the lock; no cross-capability foreign key';
COMMENT ON COLUMN policy.setting_locks.created_at IS 'Timestamp when the lock was added; locks do not change, they are removed';

CREATE UNIQUE INDEX IF NOT EXISTS idx_setting_locks_organization ON policy.setting_locks(organization_id, id);
-- One lock per key and level. NULLS NOT DISTINCT makes organization locks unique too.
CREATE UNIQUE INDEX IF NOT EXISTS idx_setting_locks_level_key ON policy.setting_locks(organization_id, workspace_id, key) NULLS NOT DISTINCT;

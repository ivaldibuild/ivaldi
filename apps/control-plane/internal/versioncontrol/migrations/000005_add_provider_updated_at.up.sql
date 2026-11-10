ALTER TABLE version_control.connections ADD COLUMN IF NOT EXISTS provider_updated_at TIMESTAMPTZ NULL;
ALTER TABLE version_control.sources ADD COLUMN IF NOT EXISTS provider_updated_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN version_control.connections.provider_updated_at IS 'Provider timestamp of the newest webhook applied to the connection; older webhooks are ignored so out-of-order deliveries cannot undo newer ones';
COMMENT ON COLUMN version_control.sources.provider_updated_at IS 'Provider timestamp of the newest webhook applied to the source; older webhooks are ignored so out-of-order deliveries cannot undo newer ones';

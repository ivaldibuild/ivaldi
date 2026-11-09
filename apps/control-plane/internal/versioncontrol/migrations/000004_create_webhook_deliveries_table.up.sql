CREATE TABLE IF NOT EXISTS version_control.webhook_deliveries (
  provider VARCHAR(50) NOT NULL,
  route_key VARCHAR(512) NOT NULL,
  delivery_id VARCHAR(255) NOT NULL,
  organization_id VARCHAR(30) NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, route_key, delivery_id)
);

COMMENT ON TABLE version_control.webhook_deliveries IS 'Provider webhook deliveries already handled, keyed platform-wide so a redelivery is recognized; intentionally not tenant-filtered';
COMMENT ON COLUMN version_control.webhook_deliveries.provider IS 'Provider that sent the delivery, e.g., github, gitlab, bitbucket, gitea, azure_devops';
COMMENT ON COLUMN version_control.webhook_deliveries.route_key IS 'Platform-wide webhook route key the delivery arrived on, e.g., github:github.com:42';
COMMENT ON COLUMN version_control.webhook_deliveries.delivery_id IS 'Provider''s delivery ID; unique per provider and route key';
COMMENT ON COLUMN version_control.webhook_deliveries.organization_id IS 'ID of the organization the route key resolved to; no cross-capability foreign key';
COMMENT ON COLUMN version_control.webhook_deliveries.received_at IS 'Timestamp when the delivery was first recorded';

ALTER TABLE delivery.deployments
  ADD COLUMN IF NOT EXISTS approval_request_id VARCHAR(30) NULL;

COMMENT ON COLUMN delivery.deployments.approval_request_id IS 'ID of the policy approval request the deployment waited for, opened when it was requested in awaiting_approval; NULL for a deployment that did not wait; no cross-capability foreign key';

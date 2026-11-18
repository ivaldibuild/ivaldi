CREATE TABLE IF NOT EXISTS policy.approval_requirements (
  organization_id VARCHAR(30) NOT NULL,
  approval_request_id VARCHAR(30) NOT NULL,
  permission VARCHAR(100) NOT NULL,
  resource_id VARCHAR(30) NOT NULL,
  resource_path TEXT[] NOT NULL,
  PRIMARY KEY (approval_request_id, permission, resource_id),
  FOREIGN KEY (organization_id, approval_request_id) REFERENCES policy.approval_requests (organization_id, id)
);

COMMENT ON TABLE policy.approval_requirements IS 'What deciding an approval request needs: a decider must hold every one of its requirements, each a permission on an IAM resource';
COMMENT ON COLUMN policy.approval_requirements.organization_id IS 'ID of the organization the request belongs to';
COMMENT ON COLUMN policy.approval_requirements.approval_request_id IS 'ID of the approval request';
COMMENT ON COLUMN policy.approval_requirements.permission IS 'Permission a decider must hold, such as deployment.create';
COMMENT ON COLUMN policy.approval_requirements.resource_id IS 'IAM resource the permission must be held on, or above; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requirements.resource_path IS 'The resource and its ancestors up to the organization, read from IAM when the request was opened, so listing can match grants on any of them';

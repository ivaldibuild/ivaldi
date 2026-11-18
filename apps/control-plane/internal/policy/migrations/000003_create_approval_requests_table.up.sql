CREATE TABLE IF NOT EXISTS policy.approval_requests (
  id VARCHAR(30) PRIMARY KEY,
  organization_id VARCHAR(30) NOT NULL,
  subject_type VARCHAR(100) NOT NULL,
  subject_id VARCHAR(300) NOT NULL,
  state VARCHAR(20) NOT NULL DEFAULT 'pending',
  decision_id VARCHAR(30) NOT NULL,
  rule VARCHAR(255) NOT NULL,
  reason VARCHAR(1000) NOT NULL,
  action VARCHAR(100) NOT NULL,
  permission VARCHAR(100) NOT NULL,
  resource_id VARCHAR(30) NOT NULL,
  proposal_digest CHAR(64) NOT NULL,
  summary JSONB NOT NULL,
  read_resource_id VARCHAR(30) NOT NULL,
  owner_team_id VARCHAR(30) NULL,
  requested_by_principal_id VARCHAR(30) NOT NULL,
  session_id VARCHAR(30) NULL,
  on_behalf_of_principal_id VARCHAR(30) NULL,
  closed_by_principal_id VARCHAR(30) NULL,
  closed_decision_id VARCHAR(30) NULL,
  note VARCHAR(2000) NULL,
  closed_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_approval_requests_subject_type CHECK (subject_type ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$'),
  CONSTRAINT chk_approval_requests_state CHECK (state IN ('pending', 'approved', 'rejected', 'cancelled')),
  CONSTRAINT chk_approval_requests_summary_size CHECK (octet_length(summary::text) <= 65536),
  -- A pending request has not been closed; any other state records who closed it, under which decision, and when.
  CONSTRAINT chk_approval_requests_closed CHECK (
    (state = 'pending') = (closed_at IS NULL)
    AND (closed_at IS NULL) = (closed_by_principal_id IS NULL)
    AND (closed_at IS NULL) = (closed_decision_id IS NULL)
  )
);

COMMENT ON TABLE policy.approval_requests IS 'Requests for a human''s approval: what waits (its subject, owned by another capability), the decision and rule that made it wait, who asked, and who approved, rejected, or cancelled it. Rows are never deleted';
COMMENT ON COLUMN policy.approval_requests.id IS 'Primary key of the request (apr_ prefixed ULID)';
COMMENT ON COLUMN policy.approval_requests.organization_id IS 'ID of the organization the request belongs to; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.subject_type IS 'Type of what waits, registered by the capability that owns it, such as delivery.deployment';
COMMENT ON COLUMN policy.approval_requests.subject_id IS 'ID of what waits, in the capability that owns it; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.state IS 'pending while the subject waits; approved, rejected, or cancelled once closed, which is final';
COMMENT ON COLUMN policy.approval_requests.decision_id IS 'ID of the authorizer''s requires_approval decision (dcn_ prefixed ULID)';
COMMENT ON COLUMN policy.approval_requests.rule IS 'Stable identifier of the rule that required approval, such as builtin.agent_session_release';
COMMENT ON COLUMN policy.approval_requests.reason IS 'Why the rule required approval, for approvers';
COMMENT ON COLUMN policy.approval_requests.action IS 'Action of the authorization request that waits, such as deployment.create';
COMMENT ON COLUMN policy.approval_requests.permission IS 'Permission of the authorization request that waits';
COMMENT ON COLUMN policy.approval_requests.resource_id IS 'IAM resource of the authorization request that waits; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.proposal_digest IS 'Hex SHA-256 of the waiting authorization request''s proposal as JSON, so an approval covers only what was asked';
COMMENT ON COLUMN policy.approval_requests.summary IS 'What approvers are shown, as the subject''s capability described it when the request was opened';
COMMENT ON COLUMN policy.approval_requests.read_resource_id IS 'IAM resource approval.read is checked on: whoever can read the subject can read its request; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.owner_team_id IS 'ID of the team that owned the subject when the request was opened, which it is routed to; NULL when the subject has no owning team; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.requested_by_principal_id IS 'ID of the principal that asked: a user, a service account, or an agent session''s principal; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.session_id IS 'ID of the orchestration session that asked, if an agent session asked; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.on_behalf_of_principal_id IS 'ID of the principal the requesting agent session acts for; NULL unless an agent session asked; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.closed_by_principal_id IS 'ID of the principal that approved, rejected, or cancelled the request; NULL while pending; no cross-capability foreign key';
COMMENT ON COLUMN policy.approval_requests.closed_decision_id IS 'ID of the decision that allowed closing the request; NULL while pending';
COMMENT ON COLUMN policy.approval_requests.note IS 'Why it was approved, rejected, or cancelled, if given';
COMMENT ON COLUMN policy.approval_requests.closed_at IS 'Timestamp when the request was approved, rejected, or cancelled; NULL while pending';
COMMENT ON COLUMN policy.approval_requests.created_at IS 'Timestamp when the request was opened';
COMMENT ON COLUMN policy.approval_requests.updated_at IS 'Timestamp when the request last changed';

CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_requests_organization ON policy.approval_requests(organization_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_requests_decision ON policy.approval_requests(decision_id);
-- A subject has at most one pending request.
CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_requests_pending_subject
  ON policy.approval_requests(organization_id, subject_type, subject_id) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS idx_approval_requests_list ON policy.approval_requests(organization_id, state, created_at, id);

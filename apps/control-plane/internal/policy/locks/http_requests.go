package locks

import "github.com/ivaldibuild/ivaldi-control-plane/internal/policy"

// LockScopeParams name the level a request is about: the caller's
// Organization, or a Workspace in it.
type LockScopeParams struct {
	WorkspaceID string `query:"workspace_id" doc:"The Workspace, for a lock on its Projects; omit for an Organization lock on every Workspace and Project"`
}

func (p LockScopeParams) scope(organizationID string) policy.LockScope {
	return policy.LockScope{OrganizationID: organizationID, WorkspaceID: p.WorkspaceID}
}

type ListSettingLocksRequest struct {
	LockScopeParams
}

type SettingLockRequest struct {
	Key string `path:"key" doc:"The setting's key, such as delivery.default_lifecycle"`
	LockScopeParams
}

func (r *SettingLockRequest) input(organizationID string) policy.SettingLockInput {
	return policy.SettingLockInput{Scope: r.scope(organizationID), Key: r.Key}
}

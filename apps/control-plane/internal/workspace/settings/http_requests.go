package settings

import (
	"encoding/json"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// ScopeParams name the level a request is about: the caller's
// Organization, a Workspace in it, or a Project in that Workspace.
type ScopeParams struct {
	WorkspaceID string `query:"workspace_id" doc:"The Workspace, for a Workspace or Project level; omit for the Organization"`
	ProjectID   string `query:"project_id" doc:"The Project, for a Project level; needs its workspace_id"`
}

func (p ScopeParams) scope(organizationID string) workspace.Scope {
	return workspace.Scope{OrganizationID: organizationID, WorkspaceID: p.WorkspaceID, ProjectID: p.ProjectID}
}

type ListSettingsRequest struct {
	ScopeParams
}

type SettingRequest struct {
	Key string `path:"key" doc:"The setting's key, such as delivery.default_lifecycle"`
	ScopeParams
}

type ChangeSettingRequest struct {
	Key string `path:"key" doc:"The setting's key, such as delivery.default_lifecycle"`
	ScopeParams
	Body struct {
		Value           json.RawMessage `json:"value" required:"true" doc:"The new value, as JSON"`
		ExpectedVersion int64           `json:"expected_version" required:"true" minimum:"0" doc:"The version the value was read at; 0 when the level has none"`
		ApprovalID      string          `json:"approval_id,omitempty" required:"false" doc:"The approved decision, when resubmitting a change that needed approval"`
	}
}

func (r *ChangeSettingRequest) input(organizationID string) workspace.ChangeSettingInput {
	return workspace.ChangeSettingInput{
		Scope:           r.scope(organizationID),
		Key:             r.Key,
		Value:           r.Body.Value,
		ExpectedVersion: r.Body.ExpectedVersion,
		ApprovalID:      r.Body.ApprovalID,
	}
}

type RemoveSettingRequest struct {
	Key string `path:"key" doc:"The setting's key, such as delivery.default_lifecycle"`
	ScopeParams
	ExpectedVersion int64  `query:"expected_version" required:"true" minimum:"1" doc:"The version the value was read at"`
	ApprovalID      string `query:"approval_id" doc:"The approved decision, when resubmitting a removal that needed approval"`
}

func (r *RemoveSettingRequest) input(organizationID string) workspace.RemoveSettingInput {
	return workspace.RemoveSettingInput{
		Scope:           r.scope(organizationID),
		Key:             r.Key,
		ExpectedVersion: r.ExpectedVersion,
		ApprovalID:      r.ApprovalID,
	}
}

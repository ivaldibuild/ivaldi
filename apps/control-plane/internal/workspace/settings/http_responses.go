package settings

import (
	"encoding/json"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

type SettingResponse struct {
	Body api.Response[workspace.SettingSnapshot]
}

type SettingListResponse struct {
	Body api.Response[[]workspace.SettingSnapshot]
}

// Resolved is a setting's effective value at a level.
type Resolved struct {
	Key   string          `json:"key" doc:"The setting's key"`
	Value json.RawMessage `json:"value" doc:"The effective value; null when there is none"`
	// Level is empty when the value is the definition's default.
	Level       workspace.Level `json:"level,omitempty" enum:"organization,workspace,project" doc:"The level the value is stored at; empty for the default"`
	FromDefault bool            `json:"from_default" doc:"Whether the value is the setting's default"`
	Locked      bool            `json:"locked" doc:"Whether a lock fixed the value at a higher level"`
}

type ResolvedResponse struct {
	Body api.Response[Resolved]
}

func settingResponse(s *workspace.Setting) *SettingResponse {
	return &SettingResponse{Body: api.SuccessResponse(nil, s.Snapshot())}
}

func settingListResponse(settings []*workspace.Setting) *SettingListResponse {
	items := make([]workspace.SettingSnapshot, 0, len(settings))
	for _, s := range settings {
		items = append(items, s.Snapshot())
	}
	return &SettingListResponse{Body: api.SuccessResponse(nil, items)}
}

func resolvedResponse(r workspace.Resolved) *ResolvedResponse {
	value := r.Value
	if value == nil {
		value = json.RawMessage(`null`)
	}
	return &ResolvedResponse{Body: api.SuccessResponse(nil, Resolved{
		Key: r.Key, Value: value, Level: r.Level, FromDefault: r.FromDefault(), Locked: r.Locked,
	})}
}

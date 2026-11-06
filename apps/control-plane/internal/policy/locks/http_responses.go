package locks

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
)

type SettingLockResponse struct {
	Body api.Response[policy.SettingLockSnapshot]
}

type SettingLockListResponse struct {
	Body api.Response[[]policy.SettingLockSnapshot]
}

func settingLockResponse(l *policy.SettingLock) *SettingLockResponse {
	return &SettingLockResponse{Body: api.SuccessResponse(nil, l.Snapshot())}
}

func settingLockListResponse(locks []*policy.SettingLock) *SettingLockListResponse {
	items := make([]policy.SettingLockSnapshot, 0, len(locks))
	for _, l := range locks {
		items = append(items, l.Snapshot())
	}
	return &SettingLockListResponse{Body: api.SuccessResponse(nil, items)}
}

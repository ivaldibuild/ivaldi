package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
)

type settingLockMapper struct{}

func (settingLockMapper) ToPersistence(l *policy.SettingLock) *settingLockEntity {
	s := l.Snapshot()
	e := &settingLockEntity{
		ID:                  s.ID,
		OrganizationID:      s.OrganizationID,
		Key:                 s.Key,
		LockedByPrincipalID: s.LockedByPrincipalID,
		CreatedAt:           s.CreatedAt,
	}
	if s.WorkspaceID != "" {
		e.WorkspaceID = &s.WorkspaceID
	}
	return e
}

func (settingLockMapper) ToDomain(e *settingLockEntity) (*policy.SettingLock, error) {
	scope := policy.OrganizationLockScope(e.OrganizationID)
	if e.WorkspaceID != nil {
		scope.WorkspaceID = *e.WorkspaceID
	}
	created := e.CreatedAt.UTC()
	return policy.NewSettingLock(policy.NewSettingLockProps{
		Scope:               scope,
		Key:                 e.Key,
		LockedByPrincipalID: e.LockedByPrincipalID,
		CreatedAt:           &created,
	}, &e.ID)
}

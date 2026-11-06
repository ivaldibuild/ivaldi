package server

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	deliverypg "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/locks"
	policypg "github.com/ivaldibuild/ivaldi-control-plane/internal/policy/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
)

// wireSettings builds what every capability shares about settings: the
// registry each capability defines its settings in, the policy
// capability's locks on them, and the resolver capabilities read them
// through. It also serves the lock API. It runs before the capabilities
// that define or read settings.
func (a *app) wireSettings() error {
	a.settingDefs = workspace.NewDefinitions()
	if err := delivery.RegisterSettings(a.settingDefs, deliverypg.NewLifecycleRepository(a.db)); err != nil {
		return err
	}

	lockRepo := policypg.NewSettingLockRepository(a.db)
	a.settingLocks = policy.NewLockSource(lockRepo)
	workspaceRepo := workspacepg.NewRepository(a.db)
	resolver, err := workspace.NewSettingResolver(workspaceRepo, a.settingDefs, a.settingLocks)
	if err != nil {
		return err
	}
	a.settingResolver = resolver

	lockSvc, err := locks.NewSettingLockService(
		locks.WithSettingLockRepo(lockRepo),
		locks.WithDefinitions(a.settingDefs),
		locks.WithWorkspaces(workspace.NewLookups(workspaceRepo)),
		locks.WithAuthorizer(a.authz),
		locks.WithTransactor(a.tx),
		locks.WithOutboxRecorder(a.outbox),
		locks.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.settingLocks = lockSvc
	return nil
}

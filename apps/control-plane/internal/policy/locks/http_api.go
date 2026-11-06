package locks

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SettingLockController serves the setting lock operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request. The level is the Organization unless the query
// names a Workspace.
type SettingLockController struct {
	service policy.SettingLockService
}

var _ api.Routes = (*SettingLockController)(nil)

func NewSettingLockController(service policy.SettingLockService) *SettingLockController {
	return &SettingLockController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *SettingLockController) RegisterRoutes(h huma.API) {
	register(h, "list-setting-locks", http.MethodGet, "/setting-locks", "List the setting locks set at one level", 0, c.ListSettingLocks)
	register(h, "add-setting-lock", http.MethodPut, "/setting-locks/{key}", "Lock a setting for every level below this one", 0, c.AddSettingLock)
	register(h, "remove-setting-lock", http.MethodDelete, "/setting-locks/{key}", "Remove a setting lock, so the values below it apply again", http.StatusNoContent, c.RemoveSettingLock)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"policy"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

// caller returns the authenticated caller. The API refuses a secured
// operation without a valid token, so a missing caller is a wiring bug.
func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("an access token is required")
	}
	return c, nil
}

func (c *SettingLockController) ListSettingLocks(ctx context.Context, req *ListSettingLocksRequest) (*SettingLockListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	locks, err := c.service.ListSettingLocks(ctx, who.Actor, req.scope(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return settingLockListResponse(locks), nil
}

func (c *SettingLockController) AddSettingLock(ctx context.Context, req *SettingLockRequest) (*SettingLockResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	l, err := c.service.AddSettingLock(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return settingLockResponse(l), nil
}

func (c *SettingLockController) RemoveSettingLock(ctx context.Context, req *SettingLockRequest) (*struct{}, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.RemoveSettingLock(ctx, who.Actor, req.input(who.OrganizationID)); err != nil {
		return nil, httpError(ctx, err)
	}
	return nil, nil
}

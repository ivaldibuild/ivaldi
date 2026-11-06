package settings

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// SettingController serves the setting operations. Every operation requires
// an access token; the caller's Organization comes from the token, never
// from the request. The level is the Organization unless the query names a
// Workspace, and a Project in it.
type SettingController struct {
	service workspace.SettingService
}

var _ api.Routes = (*SettingController)(nil)

func NewSettingController(service workspace.SettingService) *SettingController {
	return &SettingController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *SettingController) RegisterRoutes(h huma.API) {
	register(h, "list-settings", http.MethodGet, "/settings", "List the settings stored at one level", 0, c.ListSettings)
	register(h, "resolve-setting", http.MethodGet, "/settings/{key}/resolved", "Get a setting's effective value at a level", 0, c.ResolveSetting)
	register(h, "change-setting", http.MethodPut, "/settings/{key}", "Set a setting's value at one level", 0, c.ChangeSetting)
	register(h, "remove-setting", http.MethodDelete, "/settings/{key}", "Remove a setting's value from one level, so the value above applies", http.StatusNoContent, c.RemoveSetting)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"settings"},
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

func (c *SettingController) ListSettings(ctx context.Context, req *ListSettingsRequest) (*SettingListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := c.service.ListSettings(ctx, who.Actor, req.scope(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return settingListResponse(settings), nil
}

func (c *SettingController) ResolveSetting(ctx context.Context, req *SettingRequest) (*ResolvedResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	r, err := c.service.ResolveSetting(ctx, who.Actor, req.scope(who.OrganizationID), req.Key)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return resolvedResponse(r), nil
}

func (c *SettingController) ChangeSetting(ctx context.Context, req *ChangeSettingRequest) (*SettingResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	s, err := c.service.ChangeSetting(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return settingResponse(s), nil
}

func (c *SettingController) RemoveSetting(ctx context.Context, req *RemoveSettingRequest) (*struct{}, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.service.RemoveSetting(ctx, who.Actor, req.input(who.OrganizationID)); err != nil {
		return nil, httpError(ctx, err)
	}
	return nil, nil
}

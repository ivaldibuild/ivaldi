package registries

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// RegistryController serves the Registry operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request.
type RegistryController struct {
	service delivery.RegistryService
}

var _ api.Routes = (*RegistryController)(nil)

func NewRegistryController(service delivery.RegistryService) *RegistryController {
	return &RegistryController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *RegistryController) RegisterRoutes(h huma.API) {
	register(h, "create-registry", http.MethodPost, "/workspaces/{workspace_id}/registries", "Create a Registry in a Workspace", http.StatusCreated, c.CreateRegistry)
	register(h, "list-registries", http.MethodGet, "/workspaces/{workspace_id}/registries", "List a Workspace's Registries", 0, c.ListRegistries)
	register(h, "get-registry", http.MethodGet, "/registries/{registry_id}", "Get a Registry", 0, c.GetRegistry)
	register(h, "update-registry", http.MethodPatch, "/registries/{registry_id}", "Change a Registry", 0, c.UpdateRegistry)
	register(h, "set-registry-credential", http.MethodPut, "/registries/{registry_id}/credential", "Set or clear the credential a Registry's lookups use", 0, c.SetRegistryCredential)
	register(h, "set-registry-lookup", http.MethodPut, "/registries/{registry_id}/lookup", "Set where a Registry's lookups run", 0, c.SetRegistryLookup)
	register(h, "archive-registry", http.MethodPost, "/registries/{registry_id}/archive", "Archive a Registry", 0, c.ArchiveRegistry)
	register(h, "restore-registry", http.MethodPost, "/registries/{registry_id}/restore", "Restore an archived Registry", 0, c.RestoreRegistry)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"registries"},
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

func (c *RegistryController) CreateRegistry(ctx context.Context, req *CreateRegistryRequest) (*RegistryResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	r, err := c.service.CreateRegistry(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return registryResponse(r), nil
}

func (c *RegistryController) ListRegistries(ctx context.Context, req *ListRegistriesRequest) (*RegistryListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := c.service.ListRegistries(ctx, who.Actor, delivery.ListRegistriesQuery{
		OrganizationID:  who.OrganizationID,
		WorkspaceID:     req.WorkspaceID,
		IncludeArchived: req.IncludeArchived,
		Page:            req.query(),
	})
	if err != nil {
		return nil, httpError(ctx, err)
	}
	out := &RegistryListResponse{}
	out.Body = api.SuccessResponse(nil, snapshots(page, (*delivery.Registry).Snapshot))
	return out, nil
}

func (c *RegistryController) GetRegistry(ctx context.Context, req *RegistryRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, c.service.GetRegistry)
}

func (c *RegistryController) UpdateRegistry(ctx context.Context, req *UpdateRegistryRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, func(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
		return c.service.UpdateRegistry(ctx, actor, organizationID, registryID, req.props())
	})
}

func (c *RegistryController) SetRegistryCredential(ctx context.Context, req *SetRegistryCredentialRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, func(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
		return c.service.SetRegistryCredential(ctx, actor, organizationID, registryID, req.Body.CredentialRef)
	})
}

func (c *RegistryController) SetRegistryLookup(ctx context.Context, req *SetRegistryLookupRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, func(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*delivery.Registry, error) {
		return c.service.SetRegistryLookup(ctx, actor, organizationID, registryID, delivery.LookupFrom(req.Body.LookupFrom), req.pool())
	})
}

func (c *RegistryController) ArchiveRegistry(ctx context.Context, req *RegistryRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, c.service.ArchiveRegistry)
}

func (c *RegistryController) RestoreRegistry(ctx context.Context, req *RegistryRequest) (*RegistryResponse, error) {
	return c.registry(ctx, req.RegistryID, c.service.RestoreRegistry)
}

// registry calls an operation on one Registry.
func (c *RegistryController) registry(ctx context.Context, registryID string, op func(context.Context, identity.Actor, string, string) (*delivery.Registry, error)) (*RegistryResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	r, err := op(ctx, who.Actor, who.OrganizationID, registryID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return registryResponse(r), nil
}

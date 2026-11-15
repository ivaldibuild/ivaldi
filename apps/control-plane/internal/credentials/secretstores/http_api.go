package secretstores

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// SecretStoreController serves the Secret Store operations. Every operation
// requires an access token; the caller's Organization comes from the token,
// never from the request.
type SecretStoreController struct {
	service credentials.SecretStoreService
}

var _ api.Routes = (*SecretStoreController)(nil)

func NewSecretStoreController(service credentials.SecretStoreService) *SecretStoreController {
	return &SecretStoreController{service: service}
}

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *SecretStoreController) RegisterRoutes(h huma.API) {
	register(h, "create-secret-store", http.MethodPost, "/workspaces/{workspace_id}/secret-stores", "Create a Secret Store in a Workspace", http.StatusCreated, c.CreateSecretStore)
	register(h, "list-secret-stores", http.MethodGet, "/workspaces/{workspace_id}/secret-stores", "List a Workspace's Secret Stores", 0, c.ListSecretStores)
	register(h, "get-secret-store", http.MethodGet, "/secret-stores/{secret_store_id}", "Get a Secret Store", 0, c.GetSecretStore)
	register(h, "update-secret-store", http.MethodPatch, "/secret-stores/{secret_store_id}", "Change a Secret Store", 0, c.UpdateSecretStore)
	register(h, "archive-secret-store", http.MethodPost, "/secret-stores/{secret_store_id}/archive", "Archive a Secret Store no variable refers to", 0, c.ArchiveSecretStore)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Tags:          []string{"secret-stores"},
		DefaultStatus: status,
		Security:      bearer,
	}, handler)
}

// SettingsBody is a store's non-secret settings: only the block for its kind.
type SettingsBody struct {
	AWS     *credentials.AWSSettings     `json:"aws,omitempty" required:"false" doc:"For aws_secrets_manager: the region, and a role to assume on top of the agent's identity"`
	Vault   *credentials.VaultSettings   `json:"vault,omitempty" required:"false" doc:"For vault: the address, KV v2 mount, and Kubernetes auth role"`
	Doppler *credentials.DopplerSettings `json:"doppler,omitempty" required:"false" doc:"For doppler: the Secret in the agent's namespace that holds the service token"`
}

func (b *SettingsBody) settings() credentials.Settings {
	if b == nil {
		return credentials.Settings{}
	}
	return credentials.Settings{AWS: b.AWS, Vault: b.Vault, Doppler: b.Doppler}
}

type CreateSecretStoreRequest struct {
	WorkspaceID string `path:"workspace_id" doc:"The Workspace the Secret Store belongs to"`
	Body        struct {
		DisplayName    string        `json:"display_name" required:"true" minLength:"1" doc:"The display name for the store"`
		Slug           *string       `json:"slug,omitempty" required:"false" doc:"Url slug for the store; derived from the display name when omitted"`
		Kind           string        `json:"kind" required:"true" enum:"kubernetes,aws_secrets_manager,vault,doppler" doc:"The secret manager"`
		Settings       *SettingsBody `json:"settings,omitempty" required:"false" doc:"The kind's settings; none for kubernetes"`
		EnvironmentIDs []string      `json:"environment_ids,omitempty" required:"false" doc:"The Environments whose Deployments may read the store; every Environment in the Workspace when omitted. Required for doppler"`
	}
}

type ListSecretStoresRequest struct {
	WorkspaceID     string `path:"workspace_id" doc:"The Workspace to list Secret Stores in"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived Secret Stores"`
}

type SecretStoreRequest struct {
	SecretStoreID string `path:"secret_store_id" doc:"The Secret Store's ID"`
}

type UpdateSecretStoreRequest struct {
	SecretStoreID string `path:"secret_store_id" doc:"The Secret Store's ID"`
	Body          struct {
		DisplayName    *string       `json:"display_name,omitempty" required:"false" minLength:"1" doc:"New display name"`
		Slug           *string       `json:"slug,omitempty" required:"false" doc:"New slug"`
		Settings       *SettingsBody `json:"settings,omitempty" required:"false" doc:"New settings for the store's kind"`
		EnvironmentIDs *[]string     `json:"environment_ids,omitempty" required:"false" doc:"The Environments that may read the store, replacing the list; empty allows every Environment"`
	}
}

type SecretStoreResponse struct {
	Body api.Response[credentials.SecretStoreSnapshot]
}

type SecretStoreListResponse struct {
	Body api.Response[[]credentials.SecretStoreSnapshot]
}

func storeResponse(s *credentials.SecretStore) *SecretStoreResponse {
	return &SecretStoreResponse{Body: api.SuccessResponse(nil, s.Snapshot())}
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

func (c *SecretStoreController) CreateSecretStore(ctx context.Context, req *CreateSecretStoreRequest) (*SecretStoreResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	in := credentials.CreateSecretStoreInput{
		OrganizationID: who.OrganizationID, WorkspaceID: req.WorkspaceID, DisplayName: req.Body.DisplayName,
		Kind: secretstore.Kind(req.Body.Kind), Settings: req.Body.Settings.settings(), EnvironmentIDs: req.Body.EnvironmentIDs,
	}
	if req.Body.Slug != nil {
		in.Slug = *req.Body.Slug
	}
	store, err := c.service.CreateSecretStore(ctx, who.Actor, in)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return storeResponse(store), nil
}

func (c *SecretStoreController) ListSecretStores(ctx context.Context, req *ListSecretStoresRequest) (*SecretStoreListResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := c.service.ListSecretStores(ctx, who.Actor, who.OrganizationID, req.WorkspaceID, req.IncludeArchived)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	items := make([]credentials.SecretStoreSnapshot, 0, len(stores))
	for _, s := range stores {
		items = append(items, s.Snapshot())
	}
	return &SecretStoreListResponse{Body: api.SuccessResponse(nil, items)}, nil
}

func (c *SecretStoreController) GetSecretStore(ctx context.Context, req *SecretStoreRequest) (*SecretStoreResponse, error) {
	return c.store(ctx, req.SecretStoreID, c.service.GetSecretStore)
}

func (c *SecretStoreController) UpdateSecretStore(ctx context.Context, req *UpdateSecretStoreRequest) (*SecretStoreResponse, error) {
	return c.store(ctx, req.SecretStoreID, func(ctx context.Context, actor identity.Actor, organizationID, storeID string) (*credentials.SecretStore, error) {
		props := credentials.UpdateSecretStoreProps{Slug: req.Body.Slug, DisplayName: req.Body.DisplayName, EnvironmentIDs: req.Body.EnvironmentIDs}
		if req.Body.Settings != nil {
			props.Settings = new(req.Body.Settings.settings())
		}
		return c.service.UpdateSecretStore(ctx, actor, organizationID, storeID, props)
	})
}

func (c *SecretStoreController) ArchiveSecretStore(ctx context.Context, req *SecretStoreRequest) (*SecretStoreResponse, error) {
	return c.store(ctx, req.SecretStoreID, c.service.ArchiveSecretStore)
}

// store calls an operation on one Secret Store.
func (c *SecretStoreController) store(ctx context.Context, storeID string, op func(context.Context, identity.Actor, string, string) (*credentials.SecretStore, error)) (*SecretStoreResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	s, err := op(ctx, who.Actor, who.OrganizationID, storeID)
	if err != nil {
		return nil, httpError(ctx, err)
	}
	return storeResponse(s), nil
}

package secretstores

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
)

type SecretStoreResponse struct {
	Body api.Response[credentials.SecretStoreSnapshot]
}

type SecretStoreListResponse struct {
	Body api.Response[[]credentials.SecretStoreSnapshot]
}

func storeResponse(s *credentials.SecretStore) *SecretStoreResponse {
	return &SecretStoreResponse{Body: api.SuccessResponse(nil, s.Snapshot())}
}

package variables

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type VariableResponse struct {
	Body api.Response[delivery.VariableSnapshot]
}

type VariableListResponse struct {
	Body api.Response[[]delivery.VariableSnapshot]
}

func variableResponse(v *delivery.Variable) *VariableResponse {
	return &VariableResponse{Body: api.SuccessResponse(nil, v.Snapshot())}
}

func variableListResponse(vs []*delivery.Variable) *VariableListResponse {
	items := make([]delivery.VariableSnapshot, 0, len(vs))
	for _, v := range vs {
		items = append(items, v.Snapshot())
	}
	return &VariableListResponse{Body: api.SuccessResponse(nil, items)}
}

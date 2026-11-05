package httpapi

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type RestController struct {
	service delivery.Service
}

func NewRestController(service delivery.Service) *RestController {
	return &RestController{
		service: service,
	}
}

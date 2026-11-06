package api

type RequestStatus string

const (
	ApiRequestStatusSuccess    RequestStatus = "success"
	ApiRequestStatusProcessing RequestStatus = "processing"
	ApiRequestStatusError      RequestStatus = "error"
)

// Response is the standard envelope for all API responses.
type Response[T any] struct {
	Status  RequestStatus `json:"status" doc:"Request status"`
	Message string        `json:"message,omitempty" doc:"Human-readable message"`
	Data    T             `json:"data,omitempty" doc:"Response payload"`
}

func SuccessResponse[T any](message *string, data T) Response[T] {
	response := Response[T]{
		Status: ApiRequestStatusSuccess,
		Data:   data,
	}

	if message != nil {
		response.Message = *message
	}

	return response
}

func ProcessingResponse[T any](message *string, data T) Response[T] {
	response := Response[T]{
		Status: ApiRequestStatusProcessing,
		Data:   data,
	}

	if message != nil {
		response.Message = *message
	}

	return response
}

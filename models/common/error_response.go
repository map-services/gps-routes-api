package common

// ErrorResponse represents a consistent API error response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the error code and human-readable message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewErrorResponse creates an ErrorResponse with the given code and message.
func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	}
}

package common

// ErrorResponse represents a consistent API error response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the error code and human-readable message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
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

// NewValidationError creates an ErrorResponse for a validation failure on a specific field.
func NewValidationError(field, message string) ErrorResponse {
	return ErrorResponse{
		Error: ErrorDetail{
			Code:    "VALIDATION_ERROR",
			Message: message,
			Field:   field,
		},
	}
}

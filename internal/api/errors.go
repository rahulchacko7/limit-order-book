package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse represents a standardized error response.
type ErrorResponse struct {
	Error   string            `json:"error"`
	Message string            `json:"message"`
	Code    string            `json:"code"`
	Details map[string]string `json:"details,omitempty"`
}

// ErrorCode defines standard error codes.
type ErrorCode string

const (
	// Validation errors (4xx)
	ErrCodeInvalidRequest   ErrorCode = "INVALID_REQUEST"
	ErrCodeValidationFailed ErrorCode = "VALIDATION_FAILED"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeConflict         ErrorCode = "CONFLICT"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrCodeRateLimited      ErrorCode = "RATE_LIMITED"

	// Server errors (5xx)
	ErrCodeInternalError      ErrorCode = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"

	// Business logic errors
	ErrCodeInsufficientFunds ErrorCode = "INSUFFICIENT_FUNDS"
	ErrCodeInvalidPair       ErrorCode = "INVALID_PAIR"
	ErrCodeOrderNotFound     ErrorCode = "ORDER_NOT_FOUND"
	ErrCodeOrderCannotCancel ErrorCode = "ORDER_CANNOT_CANCEL"
	ErrCodeInvalidPrice      ErrorCode = "INVALID_PRICE"
	ErrCodeInvalidQuantity   ErrorCode = "INVALID_QUANTITY"
)

// NewErrorResponse creates a new error response.
func NewErrorResponse(code ErrorCode, message string) *ErrorResponse {
	return &ErrorResponse{
		Error:   string(code),
		Message: message,
		Code:    string(code),
	}
}

// NewErrorResponseWithDetails creates a new error response with details.
func NewErrorResponseWithDetails(code ErrorCode, message string, details map[string]string) *ErrorResponse {
	return &ErrorResponse{
		Error:   string(code),
		Message: message,
		Code:    string(code),
		Details: details,
	}
}

// AbortWithError aborts the request with a standardized error response.
func AbortWithError(c *gin.Context, status int, code ErrorCode, message string) {
	c.AbortWithStatusJSON(status, NewErrorResponse(code, message))
}

// AbortWithErrorDetails aborts the request with a standardized error response including details.
func AbortWithErrorDetails(c *gin.Context, status int, code ErrorCode, message string, details map[string]string) {
	c.AbortWithStatusJSON(status, NewErrorResponseWithDetails(code, message, details))
}

// Common error responses for reuse.
var (
	ErrInvalidRequest = NewErrorResponse(ErrCodeInvalidRequest, "Invalid request")
	ErrNotFound       = NewErrorResponse(ErrCodeNotFound, "Resource not found")
	ErrUnauthorized   = NewErrorResponse(ErrCodeUnauthorized, "Unauthorized")
	ErrInternalError  = NewErrorResponse(ErrCodeInternalError, "Internal server error")
)

// APIValidationError represents a validation error for a specific field.
type APIValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

// NewAPIValidationError creates a new validation error.
func NewAPIValidationError(field, message, value string) *APIValidationError {
	return &APIValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	}
}

// AbortWithValidationErrors aborts with multiple validation errors.
func AbortWithValidationErrors(c *gin.Context, errors []*APIValidationError) {
	details := make(map[string]string)
	for _, e := range errors {
		details[e.Field] = e.Message
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, NewErrorResponseWithDetails(
		ErrCodeValidationFailed,
		"Validation failed",
		details,
	))
}

// SuccessResponse represents a successful response.
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

// NewSuccessResponse creates a new success response.
func NewSuccessResponse(data interface{}, message string) *SuccessResponse {
	return &SuccessResponse{
		Success: true,
		Data:    data,
		Message: message,
	}
}

// PaginationParams represents pagination parameters.
type PaginationParams struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Offset  int `json:"offset"`
	Limit   int `json:"limit"`
}

// DefaultPagination returns default pagination parameters.
func DefaultPagination() *PaginationParams {
	return &PaginationParams{
		Page:    1,
		PerPage: 20,
		Offset:  0,
		Limit:   20,
	}
}

// WithPagination extracts and validates pagination parameters from query.
func WithPagination(c *gin.Context, defaultPerPage int) *PaginationParams {
	page := 1
	perPage := defaultPerPage

	if p := c.Query("page"); p != "" {
		if n := parseIntOrDefault(p, 1); n > 0 {
			page = n
		}
	}

	if pp := c.Query("per_page"); pp != "" {
		if n := parseIntOrDefault(pp, defaultPerPage); n > 0 && n <= 100 {
			perPage = n
		}
	}

	offset := (page - 1) * perPage

	return &PaginationParams{
		Page:    page,
		PerPage: perPage,
		Offset:  offset,
		Limit:   perPage,
	}
}

// PaginatedResponse represents a paginated response.
type PaginatedResponse struct {
	Success    bool        `json:"success"`
	Data       interface{} `json:"data"`
	Pagination *Pagination `json:"pagination"`
}

// Pagination contains pagination metadata.
type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	TotalPages int   `json:"total_pages"`
	TotalItems int64 `json:"total_items"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// NewPaginatedResponse creates a new paginated response.
func NewPaginatedResponse(data interface{}, page, perPage int, totalItems int64) *PaginatedResponse {
	totalPages := int((totalItems + int64(perPage) - 1) / int64(perPage))
	if totalPages == 0 {
		totalPages = 1
	}

	return &PaginatedResponse{
		Success: true,
		Data:    data,
		Pagination: &Pagination{
			Page:       page,
			PerPage:    perPage,
			TotalPages: totalPages,
			TotalItems: totalItems,
			HasNext:    page < totalPages,
			HasPrev:    page > 1,
		},
	}
}

// parseIntOrDefault parses a string to int, returning default on error.
func parseIntOrDefault(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return defaultVal
		}
		result = result*10 + int(c-'0')
	}
	if result == 0 {
		return defaultVal
	}
	return result
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status   string            `json:"status"`
	Version  string            `json:"version"`
	Services map[string]string `json:"services"`
}

// NewHealthResponse creates a new health response.
func NewHealthResponse(version string, services map[string]string) *HealthResponse {
	status := "healthy"
	for _, v := range services {
		if v != "healthy" {
			status = "degraded"
			break
		}
	}

	return &HealthResponse{
		Status:   status,
		Version:  version,
		Services: services,
	}
}

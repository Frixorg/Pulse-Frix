// Package httpx contains the HTTP server, router, middleware and response
// helpers for the control-plane API. See docs/API.md.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Error codes returned to clients. Never leak stack traces (spec section 65).
const (
	CodeAuth          = "AUTH_ERROR"
	CodePermission    = "PERMISSION_ERROR"
	CodeValidation    = "VALIDATION_ERROR"
	CodeNotFound      = "SERVER_NOT_FOUND"
	CodeRateLimited   = "RATE_LIMITED"
	CodeDockerUnavail = "DOCKER_UNAVAILABLE"
	CodeConfig        = "CONFIGURATION_ERROR"
	CodeSSRFBlocked   = "SSRF_BLOCKED"
	CodeInternal      = "INTERNAL_ERROR"
)

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

// JSON writes a value as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Fail writes a structured error response.
func Fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	JSON(w, status, errorEnvelope{Error: apiError{
		Code:      code,
		Message:   message,
		RequestID: RequestID(r.Context()),
	}})
}

// Page wraps a list with an optional cursor for pagination.
type Page struct {
	Data       any    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

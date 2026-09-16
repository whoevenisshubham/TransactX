package common

import (
	"encoding/json"
	"net/http"
)

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string { return e.Code }

func NewAPIError(code, message string, status int) *APIError {
	return &APIError{Code: code, Message: message, StatusCode: status}
}

func WriteError(writer http.ResponseWriter, requestID string, err *APIError) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(err.StatusCode)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"requestId": requestID,
		"error": map[string]string{
			"code":    err.Code,
			"message": err.Message,
		},
	})
}

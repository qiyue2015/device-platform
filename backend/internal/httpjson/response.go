package httpjson

import (
	"encoding/json"
	"net/http"
	"strings"
)

type nullValue struct{}

func (nullValue) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

type Response struct {
	Success   bool   `json:"success"`
	Status    int    `json:"status"`
	Message   string `json:"message"`
	Code      int    `json:"code"`
	ErrorCode string `json:"error_code,omitempty"`
	Data      any    `json:"data"`
	Meta      any    `json:"meta,omitempty"`
	RequestID string `json:"request_id"`
}

func OK(w http.ResponseWriter, data any) {
	Write(w, http.StatusOK, "ok", data)
}

func Created(w http.ResponseWriter, data any) {
	Write(w, http.StatusCreated, "created", data)
}

func Write(w http.ResponseWriter, status int, message string, data any) {
	writeEnvelope(w, status, Response{
		Success: true,
		Status:  status,
		Message: message,
		Code:    0,
		Data:    data,
		Meta:    nullValue{},
	})
}

func Error(w http.ResponseWriter, status int, errorCode, message string) {
	if strings.TrimSpace(message) == "" {
		message = errorCode
	}
	writeEnvelope(w, status, Response{
		Success:   false,
		Status:    status,
		Message:   message,
		Code:      status,
		ErrorCode: errorCode,
		Data:      nil,
	})
}

func writeEnvelope(w http.ResponseWriter, status int, body Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

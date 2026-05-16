package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type jsonResponse struct {
	Success   bool        `json:"success"`
	Status    int         `json:"status"`
	Message   string      `json:"message"`
	Code      int         `json:"code"`
	Data      interface{} `json:"data"`
	Meta      interface{} `json:"meta,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

type apiError struct {
	status  int
	code    string
	message string
}

func (e apiError) Error() string {
	return e.message
}

func newAPIError(status int, code, message string) apiError {
	return apiError{status: status, code: code, message: message}
}

func writeOK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, jsonResponse{Success: true, Status: http.StatusOK, Message: "ok", Code: 0, Data: data})
}

func writeCreated(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusCreated, jsonResponse{Success: true, Status: http.StatusCreated, Message: "created", Code: 0, Data: data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, jsonResponse{
		Success: false,
		Status:  status,
		Message: message,
		Code:    status,
		Data:    map[string]string{"error": code},
	})
}

func writeJSON(w http.ResponseWriter, status int, body jsonResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func handleError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var apiErr apiError
	if errors.As(err, &apiErr) {
		writeError(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}

	logger.Error("unhandled request error", slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

package main

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/qiyue2015/device-platform/internal/httpjson"
)

type jsonResponse = httpjson.Response

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
	httpjson.OK(w, data)
}

func writeCreated(w http.ResponseWriter, data interface{}) {
	httpjson.Created(w, data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpjson.Error(w, status, code, message)
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

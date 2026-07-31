package main

import (
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func (a *app) handleProviderCallback(_ http.ResponseWriter, r *http.Request) error {
	providerCode := strings.TrimPrefix(r.URL.Path, "/v1/provider-callbacks/")
	if providerCode == "" || strings.Contains(providerCode, "/") || providerCode != domain.ProviderCodeWWTIOT {
		return newAPIError(http.StatusNotFound, "not_found", "resource not found")
	}
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	return newAPIError(http.StatusServiceUnavailable, domain.ErrCodeProviderCallbackUnverified, "Provider callback verification is not available")
}

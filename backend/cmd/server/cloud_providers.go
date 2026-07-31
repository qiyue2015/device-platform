package main

import (
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	v1 "github.com/qiyue2015/device-platform/internal/api/v1"
	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpjson"
)

type cloudProviderConfig struct {
	Code              string `json:"code"`
	Name              string `json:"name"`
	AccessType        string `json:"access_type"`
	TransportProtocol string `json:"transport_protocol"`
	Adapter           string `json:"adapter"`
	Configured        bool   `json:"configured"`
}

type cloudProviderRegistry struct {
	providers                   []cloudProviderConfig
	wwtiotClients               map[string]*wwtiot.Client
	defaultCloudAPIProviderCode string
}

func newCloudProviderRegistry(cfg config) cloudProviderRegistry {
	const code = "wwtiot"
	const name = "WWTIOT"
	client := wwtiot.NewClient(wwtiot.Config{
		APIURL:  cfg.WWTIOTAPIURL,
		UserID:  cfg.WWTIOTUserID,
		UserKey: cfg.WWTIOTUserKey,
	}, nil)
	return cloudProviderRegistry{
		providers: []cloudProviderConfig{
			{
				Code:              code,
				Name:              name,
				AccessType:        devicecore.AccessTypeCloudAPI,
				TransportProtocol: devicecore.TransportProtocolHTTP,
				Adapter:           devicecore.AdapterWWTIOTCloudAPI,
				Configured:        client.Configured(),
			},
		},
		wwtiotClients: map[string]*wwtiot.Client{
			normalizeProviderCode(code): client,
		},
		defaultCloudAPIProviderCode: code,
	}
}

func normalizeProviderCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func (r cloudProviderRegistry) List() []cloudProviderConfig {
	providers := make([]cloudProviderConfig, len(r.providers))
	copy(providers, r.providers)
	return providers
}

func (r cloudProviderRegistry) DefaultCloudAPIProviderCode() string {
	return r.defaultCloudAPIProviderCode
}

func (r cloudProviderRegistry) HasProvider(code string) bool {
	for _, provider := range r.providers {
		if strings.EqualFold(provider.Code, strings.TrimSpace(code)) {
			return true
		}
	}
	return false
}

func (r cloudProviderRegistry) WWTIOTClient(code string) (*wwtiot.Client, bool) {
	client, ok := r.wwtiotClients[normalizeProviderCode(code)]
	return client, ok
}

func (a *app) handleCloudProviders(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	page, pageSize, err := parseProviderPagination(r.URL.RawQuery)
	if err != nil {
		return err
	}
	providers, err := a.providerResponses()
	if err != nil {
		return err
	}
	start := (page - 1) * pageSize
	if start > len(providers) {
		start = len(providers)
	}
	end := min(start+pageSize, len(providers))
	httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": providers[start:end]}, map[string]any{
		"page": page, "page_size": pageSize, "total": len(providers),
	})
	return nil
}

func (a *app) providerResponses() ([]v1.ProviderResponse, error) {
	if service := a.deviceResourceService(); service != nil {
		providers := service.ListProviders()
		result := make([]v1.ProviderResponse, 0, len(providers))
		for _, provider := range providers {
			result = append(result, v1.ProviderResponse{
				Code: provider.Code, Name: provider.Name, AccessType: provider.AccessType,
				TransportProtocol: provider.TransportProtocol, Adapter: provider.Adapter,
				IntegrationStatus: provider.IntegrationStatus,
			})
		}
		return result, nil
	}
	wwtiotStatus := domain.ProviderIntegrationUnconfigured
	if client, ok := a.cloudProviders.WWTIOTClient(domain.ProviderCodeWWTIOT); ok && client.Configured() {
		wwtiotStatus = domain.ProviderIntegrationConfiguredUnverified
	}
	return []v1.ProviderResponse{
		{
			Code: domain.ProviderCodeSimulator, Name: "Simulator", AccessType: domain.AccessTypeSimulator,
			TransportProtocol: domain.TransportProtocolInternal, Adapter: domain.AdapterSimulator,
			IntegrationStatus: domain.ProviderIntegrationVerified,
		},
		{
			Code: domain.ProviderCodeWWTIOT, Name: "WWTIOT", AccessType: domain.AccessTypeCloudAPI,
			TransportProtocol: domain.TransportProtocolHTTP, Adapter: domain.AdapterWWTIOTCloudAPI,
			IntegrationStatus: wwtiotStatus,
		},
	}, nil
}

func parseProviderPagination(rawQuery string) (int, int, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return 0, 0, newAPIError(http.StatusBadRequest, "invalid_request", "invalid query parameters")
	}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			return 0, 0, newAPIError(http.StatusBadRequest, "invalid_request", "invalid query parameters")
		}
	}
	page, err := positiveProviderQueryInteger(query, "page", 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := positiveProviderQueryInteger(query, "page_size", 20)
	if err != nil || pageSize > 100 || page-1 > math.MaxInt/pageSize {
		return 0, 0, newAPIError(http.StatusBadRequest, "invalid_request", "invalid pagination")
	}
	return page, pageSize, nil
}

func positiveProviderQueryInteger(query url.Values, key string, fallback int) (int, error) {
	values, exists := query[key]
	if !exists {
		return fallback, nil
	}
	value, err := strconv.Atoi(values[0])
	if err != nil || value < 1 {
		return 0, newAPIError(http.StatusBadRequest, "invalid_request", "invalid pagination")
	}
	return value, nil
}

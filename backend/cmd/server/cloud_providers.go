package main

import (
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
	"github.com/qiyue2015/device-platform/internal/devicecore"
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
	code := strings.TrimSpace(cfg.WWTIOTProviderCode)
	if code == "" {
		code = "wwtiot"
	}
	name := strings.TrimSpace(cfg.WWTIOTProviderName)
	if name == "" {
		name = "WWTIOT"
	}
	client := wwtiot.NewClient(wwtiot.Config{
		APIURL:  cfg.WWTIOTAPIURL,
		UserID:  cfg.WWTIOTUserID,
		UserKey: cfg.WWTIOTUserKey,
		Timeout: cfg.WWTIOTTimeout,
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
	writeOK(w, a.cloudProviders.List())
	return nil
}

package deviceservice

import (
	"context"
	"reflect"
	"testing"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type inertStore struct{}

func (inertStore) Projects() repository.ProjectQueries       { return nil }
func (inertStore) DeviceTypes() repository.DeviceTypeQueries { return nil }
func (inertStore) Devices() repository.DeviceQueries         { return nil }
func (inertStore) TransactDevice(context.Context, func(repository.DeviceTx) error) error {
	return nil
}

func TestProviderRegistryIsFixedAndEvidenceConservative(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantWWTIOT domain.ProviderIntegrationStatus
	}{
		{name: "missing credentials", config: Config{WWTIOTEndpoint: "https://gps.example.test/api/"}, wantWWTIOT: domain.ProviderIntegrationUnconfigured},
		{name: "endpoint userinfo", config: Config{WWTIOTEndpoint: "https://user@gps.example.test/api/", WWTIOTUserID: "id", WWTIOTUserKey: "key"}, wantWWTIOT: domain.ProviderIntegrationUnconfigured},
		{name: "endpoint query", config: Config{WWTIOTEndpoint: "https://gps.example.test/api/?x=1", WWTIOTUserID: "id", WWTIOTUserKey: "key"}, wantWWTIOT: domain.ProviderIntegrationUnconfigured},
		{name: "blank user id", config: Config{WWTIOTEndpoint: "https://gps.example.test/api/", WWTIOTUserID: "  ", WWTIOTUserKey: "key"}, wantWWTIOT: domain.ProviderIntegrationUnconfigured},
		{name: "complete unverified", config: Config{WWTIOTEndpoint: "https://gps.example.test/api/", WWTIOTUserID: " id ", WWTIOTUserKey: " key "}, wantWWTIOT: domain.ProviderIntegrationConfiguredUnverified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := New(inertStore{}, test.config)
			if err != nil {
				t.Fatal(err)
			}
			providers := service.ListProviders()
			if len(providers) != 2 || providers[0].Code != domain.ProviderCodeSimulator || providers[1].Code != domain.ProviderCodeWWTIOT {
				t.Fatalf("registry = %+v", providers)
			}
			if providers[1].IntegrationStatus != test.wantWWTIOT || providers[1].IntegrationStatus == domain.ProviderIntegrationVerified {
				t.Fatalf("WWTIOT status = %s", providers[1].IntegrationStatus)
			}
			if providers[0].IntegrationStatus != domain.ProviderIntegrationVerified {
				t.Fatalf("simulator status = %s", providers[0].IntegrationStatus)
			}
		})
	}
}

func TestPublicDTOsCannotExposeProviderConfiguration(t *testing.T) {
	for _, value := range []any{Provider{}, Device{}, DeviceType{}} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := typeOf.Field(index).Name
			switch name {
			case "Endpoint", "APIURL", "UserID", "UserKey", "Secret", "Password", "Configured":
				t.Fatalf("%s exposes Provider configuration field %s", typeOf.Name(), name)
			}
		}
	}
}

func TestProviderIdentityValidation(t *testing.T) {
	valid := "LOCK.alpha_1:zone-2"
	if got, err := providerIdentity(domain.ProviderCodeWWTIOT, &valid, "ignored"); err != nil || got != valid {
		t.Fatalf("valid identity = %q, %v", got, err)
	}
	for _, invalid := range []string{"", "with space", "锁-1", string(make([]byte, 129))} {
		if _, err := providerIdentity(domain.ProviderCodeWWTIOT, &invalid, "ignored"); err == nil {
			t.Fatalf("accepted invalid identity %q", invalid)
		}
	}
	simulatorID := "10000000-0000-0000-0000-000000000001"
	if got, err := providerIdentity(domain.ProviderCodeSimulator, nil, simulatorID); err != nil || got != simulatorID {
		t.Fatalf("simulator identity = %q, %v", got, err)
	}
	empty := ""
	if _, err := providerIdentity(domain.ProviderCodeSimulator, &empty, simulatorID); err == nil {
		t.Fatal("simulator accepted caller-provided identity")
	}
}

func TestLifecycleTransitionsAreClosed(t *testing.T) {
	allowed := [][2]domain.LifecycleStatus{
		{domain.LifecycleStatusActive, domain.LifecycleStatusDisabled},
		{domain.LifecycleStatusActive, domain.LifecycleStatusDeleted},
		{domain.LifecycleStatusDisabled, domain.LifecycleStatusActive},
		{domain.LifecycleStatusDisabled, domain.LifecycleStatusDeleted},
	}
	for _, pair := range allowed {
		if !canTransitionLifecycle(pair[0], pair[1]) {
			t.Fatalf("transition %s -> %s rejected", pair[0], pair[1])
		}
	}
	for _, from := range []domain.LifecycleStatus{domain.LifecycleStatusActive, domain.LifecycleStatusDisabled, domain.LifecycleStatusDeleted} {
		for _, to := range []domain.LifecycleStatus{domain.LifecycleStatusActive, domain.LifecycleStatusDisabled, domain.LifecycleStatusDeleted} {
			want := false
			for _, pair := range allowed {
				want = want || pair[0] == from && pair[1] == to
			}
			if canTransitionLifecycle(from, to) != want {
				t.Fatalf("transition %s -> %s mismatch", from, to)
			}
		}
	}
}

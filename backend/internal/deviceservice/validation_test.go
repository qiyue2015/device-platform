package deviceservice

import (
	"context"
	"reflect"
	"strings"
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

func TestProviderRegistryUsesInjectedDefinitionsAndOrder(t *testing.T) {
	registrations := []ProviderRegistration{
		testProviderRegistration("simulated", "simulated-v1", domain.ProviderIntegrationVerified),
		testProviderRegistration("direct", "direct-v2", domain.ProviderIntegrationConfiguredUnverified),
	}
	service, err := New(inertStore{}, Config{Providers: registrations})
	if err != nil {
		t.Fatal(err)
	}
	providers := service.ListProviders()
	if len(providers) != 2 || providers[0].Code != "simulated" || providers[1].Code != "direct" {
		t.Fatalf("registry = %+v", providers)
	}
	if providers[0].IntegrationStatus != domain.ProviderIntegrationVerified ||
		providers[1].IntegrationStatus != domain.ProviderIntegrationConfiguredUnverified {
		t.Fatalf("integration status = %+v", providers)
	}
	registrations[0].Provider.Profiles[0] = "mutated"
	registrations[0].Provider.ProfileActions["simulated-v1"][domain.ActionIdentifier("query_status")] = domain.ProviderActionUnsupported
	providers = service.ListProviders()
	if providers[0].Profiles[0] != "simulated-v1" ||
		providers[0].ProfileActions["simulated-v1"][domain.ActionIdentifier("query_status")] != domain.ProviderActionSupported {
		t.Fatalf("registry retained caller-owned maps: %+v", providers[0])
	}
}

func TestProviderRegistryRejectsInvalidDefinitions(t *testing.T) {
	valid := testProviderRegistration("provider", "profile-v1", domain.ProviderIntegrationUnconfigured)
	for _, test := range []struct {
		name          string
		registrations []ProviderRegistration
	}{
		{name: "empty"},
		{name: "missing identity policy", registrations: []ProviderRegistration{{Provider: valid.Provider}}},
		{name: "duplicate", registrations: []ProviderRegistration{valid, valid}},
		{name: "missing profile actions", registrations: []ProviderRegistration{{
			Provider: Provider{Code: "provider", Profiles: []string{"profile-v1"}}, IdentityPolicy: valid.IdentityPolicy,
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(inertStore{}, Config{Providers: test.registrations}); err == nil {
				t.Fatal("accepted invalid Provider registrations")
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

func TestCoreConfigurationHasNoProviderSpecificFields(t *testing.T) {
	for _, value := range []any{Config{}, ProviderRegistration{}} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			name := strings.ToLower(typeOf.Field(index).Name)
			if strings.Contains(name, "wwtiot") || strings.Contains(name, "omni") {
				t.Fatalf("%s contains Provider-specific field %s", typeOf.Name(), typeOf.Field(index).Name)
			}
		}
	}
}

func testProviderRegistration(code, profile string, status domain.ProviderIntegrationStatus) ProviderRegistration {
	return ProviderRegistration{
		Provider: Provider{
			Code: code, Name: code, Profiles: []string{profile}, IntegrationStatus: status,
			ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{
				profile: {domain.ActionIdentifier("query_status"): domain.ProviderActionSupported},
			},
		},
		IdentityPolicy: DeviceIdentityPolicyFunc(func(requested *string, platformDeviceID string) (string, error) {
			if requested == nil {
				return platformDeviceID, nil
			}
			return *requested, nil
		}),
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

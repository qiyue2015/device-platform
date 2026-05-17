package devicecore

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestService(t *testing.T) (*Service, *fakeClock, Project, Device) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)}
	service := NewServiceWithClock(clock)
	project, err := service.CreateProject(CreateProjectRequest{
		Name:       "Hotel A",
		WebhookURL: "https://example.test/webhook",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	device, err := service.CreateDevice(CreateDeviceRequest{
		ProjectID:    project.ID,
		Name:         "Front Door",
		DeviceType:   "smart_lock",
		Online:       true,
		CurrentState: map[string]any{"battery": float64(88)},
	})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	return service, clock, project, device
}

func TestProjectCreateListUpdate(t *testing.T) {
	service, _, project, _ := newTestService(t)

	projects := service.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("projects length = %d, want 1", len(projects))
	}
	renamed := "Hotel B"
	updated, err := service.UpdateProject(project.ID, UpdateProjectRequest{
		Name:        &renamed,
		IPWhitelist: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.Name != renamed || len(updated.IPWhitelist) != 1 {
		t.Fatalf("updated project = %+v", updated)
	}
	if _, err := service.ProjectByAPIKey(project.APIKey); err != nil {
		t.Fatalf("project by api key: %v", err)
	}
}

func TestDeviceCreateListDetail(t *testing.T) {
	service, _, project, device := newTestService(t)

	devices := service.ListDevices(project.ID)
	if len(devices) != 1 {
		t.Fatalf("devices length = %d, want 1", len(devices))
	}
	got, err := service.GetDevice(project.ID, device.ID)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if got.CurrentState["battery"] != float64(88) {
		t.Fatalf("current state = %#v", got.CurrentState)
	}
}

func TestCreateDeviceAcceptsCloudAPIAdapterFields(t *testing.T) {
	service, _, project, _ := newTestService(t)

	device, err := service.CreateDevice(CreateDeviceRequest{
		ProjectID:        project.ID,
		Name:             "WWTIOT Lock",
		DeviceType:       "smart_lock",
		AccessType:       AccessTypeCloudAPI,
		ProviderDeviceID: "768901037824",
	})
	if err != nil {
		t.Fatalf("create cloud api device: %v", err)
	}
	if device.AccessType != AccessTypeCloudAPI ||
		device.ProviderCode != "wwtiot" ||
		device.TransportProtocol != TransportProtocolHTTP ||
		device.Adapter != AdapterWWTIOTCloudAPI {
		t.Fatalf("unexpected cloud api defaults: %+v", device)
	}
}

func TestCreateDeviceRequiresProviderDeviceIDForCloudAPI(t *testing.T) {
	service, _, project, _ := newTestService(t)

	_, err := service.CreateDevice(CreateDeviceRequest{
		ProjectID:  project.ID,
		Name:       "WWTIOT Lock",
		DeviceType: "smart_lock",
		AccessType: AccessTypeCloudAPI,
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateDeviceRejectsMismatchedCloudAPIAdapterFields(t *testing.T) {
	service, _, project, _ := newTestService(t)

	_, err := service.CreateDevice(CreateDeviceRequest{
		ProjectID:         project.ID,
		Name:              "Bad WWTIOT Lock",
		DeviceType:        "smart_lock",
		AccessType:        AccessTypeCloudAPI,
		ProviderDeviceID:  "768901037824",
		TransportProtocol: TransportProtocolSimulator,
		Adapter:           AdapterMockGateway,
	})
	if err == nil || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateDeviceRejectsDuplicateProviderDeviceInProject(t *testing.T) {
	service, _, project, _ := newTestService(t)

	_, err := service.CreateDevice(CreateDeviceRequest{
		ProjectID:        project.ID,
		Name:             "WWTIOT Lock 1",
		DeviceType:       "smart_lock",
		AccessType:       AccessTypeCloudAPI,
		ProviderDeviceID: "768901037824",
	})
	if err != nil {
		t.Fatalf("create first cloud api device: %v", err)
	}
	_, err = service.CreateDevice(CreateDeviceRequest{
		ProjectID:        project.ID,
		Name:             "WWTIOT Lock 2",
		DeviceType:       "smart_lock",
		AccessType:       AccessTypeCloudAPI,
		ProviderCode:     "wwtiot",
		ProviderDeviceID: "768901037824",
	})
	if !errors.Is(err, ErrDuplicateDevice) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateDevice", err)
	}
}

func TestCommandIdempotencyIgnoresExpiresAt(t *testing.T) {
	service, _, project, device := newTestService(t)
	firstExpiry := time.Date(2026, 5, 16, 10, 1, 0, 0, time.UTC)
	secondExpiry := firstExpiry.Add(time.Hour)

	first, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:      project.ID,
		DeviceID:       device.ID,
		CommandType:    "query_status",
		Payload:        map[string]any{"include": "battery"},
		IdempotencyKey: "same-key",
		ExpiresAt:      &firstExpiry,
	})
	if err != nil {
		t.Fatalf("create first command: %v", err)
	}
	second, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:      project.ID,
		DeviceID:       device.ID,
		CommandType:    "query_status",
		Payload:        map[string]any{"include": "battery"},
		IdempotencyKey: "same-key",
		ExpiresAt:      &secondExpiry,
	})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent replay returned %s, want %s", second.ID, first.ID)
	}

	_, err = service.CreateCommand(CreateCommandRequest{
		ProjectID:      project.ID,
		DeviceID:       device.ID,
		CommandType:    "query_status",
		Payload:        map[string]any{"include": "signal"},
		IdempotencyKey: "same-key",
		ExpiresAt:      &firstExpiry,
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestOfflineQueueAndOnlineOnlyPolicy(t *testing.T) {
	service, _, project, device := newTestService(t)
	if err := service.SetDeviceOnline(project.ID, device.ID, false); err != nil {
		t.Fatalf("set offline: %v", err)
	}

	unlock, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "unlock",
	})
	if err != nil {
		t.Fatalf("create unlock: %v", err)
	}
	if unlock.Status != CommandStatusFailed || unlock.Reason != "device_offline" {
		t.Fatalf("unlock status = %s/%s, want failed/device_offline", unlock.Status, unlock.Reason)
	}

	query, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "query_status",
	})
	if err != nil {
		t.Fatalf("create query_status: %v", err)
	}
	if query.Status != CommandStatusOffline {
		t.Fatalf("query_status = %s, want offline", query.Status)
	}
	if err := service.SetDeviceOnline(project.ID, device.ID, true); err != nil {
		t.Fatalf("set online: %v", err)
	}
	requeued, err := service.GetCommand(project.ID, query.ID)
	if err != nil {
		t.Fatalf("get requeued command: %v", err)
	}
	if requeued.Status != CommandStatusQueued {
		t.Fatalf("requeued status = %s, want queued", requeued.Status)
	}
}

func TestExpiredOfflineCommandFailsOnRequeue(t *testing.T) {
	service, clock, project, device := newTestService(t)
	if err := service.SetDeviceOnline(project.ID, device.ID, false); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	expiresAt := clock.Now().Add(time.Second)
	command, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "query_status",
		ExpiresAt:   &expiresAt,
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	clock.Advance(2 * time.Second)
	if err := service.SetDeviceOnline(project.ID, device.ID, true); err != nil {
		t.Fatalf("set online: %v", err)
	}
	got, err := service.GetCommand(project.ID, command.ID)
	if err != nil {
		t.Fatalf("get command: %v", err)
	}
	if got.Status != CommandStatusFailed || got.Reason != "expired_while_offline" {
		t.Fatalf("status = %s/%s, want failed/expired_while_offline", got.Status, got.Reason)
	}
}

func TestReplaceLatestCancelsOlderQueuedAndOffline(t *testing.T) {
	service, _, project, device := newTestService(t)
	first, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "set_config",
		Payload:     map[string]any{"volume": float64(1)},
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "set_config",
		Payload:     map[string]any{"volume": float64(2)},
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	replaced, err := service.GetCommand(project.ID, first.ID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if replaced.Status != CommandStatusCancelled || replaced.Reason != "replaced_by_latest" {
		t.Fatalf("first status = %s/%s, want cancelled/replaced_by_latest", replaced.Status, replaced.Reason)
	}
	if second.Status != CommandStatusQueued {
		t.Fatalf("second status = %s, want queued", second.Status)
	}
}

func TestUnsafeDeliveryOverrideRejected(t *testing.T) {
	service, _, project, device := newTestService(t)
	_, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:      project.ID,
		DeviceID:       device.ID,
		CommandType:    "unlock",
		DeliveryPolicy: DeliveryPolicyQueueUntilExpire,
	})
	if !errors.Is(err, ErrUnsafeDeliveryOverride) {
		t.Fatalf("error = %v, want ErrUnsafeDeliveryOverride", err)
	}
}

func TestTimeoutCompensationOnlyForLowRiskCommands(t *testing.T) {
	service, clock, project, device := newTestService(t)
	lowRisk, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "query_status",
	})
	if err != nil {
		t.Fatalf("create low-risk command: %v", err)
	}
	lowRisk, err = service.MarkCommandSent(project.ID, lowRisk.ID)
	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if _, err := service.TimeoutCommand(project.ID, lowRisk.ID); err != nil {
		t.Fatalf("timeout low-risk: %v", err)
	}
	corrected, err := service.SucceedCommand(project.ID, lowRisk.ID, true)
	if err != nil {
		t.Fatalf("correct low-risk timeout: %v", err)
	}
	if corrected.Status != CommandStatusSuccess || !corrected.Corrected {
		t.Fatalf("corrected = %s/%v, want success/true", corrected.Status, corrected.Corrected)
	}

	physical, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "lock",
	})
	if err != nil {
		t.Fatalf("create physical command: %v", err)
	}
	physical, err = service.MarkCommandSent(project.ID, physical.ID)
	if err != nil {
		t.Fatalf("mark physical sent: %v", err)
	}
	if _, err := service.TimeoutCommand(project.ID, physical.ID); err != nil {
		t.Fatalf("timeout physical: %v", err)
	}
	_, err = service.SucceedCommand(project.ID, physical.ID, true)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("physical correction error = %v, want ErrInvalidTransition", err)
	}

	late, err := service.CreateCommand(CreateCommandRequest{
		ProjectID:   project.ID,
		DeviceID:    device.ID,
		CommandType: "query_status",
	})
	if err != nil {
		t.Fatalf("create late command: %v", err)
	}
	if _, err := service.MarkCommandSent(project.ID, late.ID); err != nil {
		t.Fatalf("mark late sent: %v", err)
	}
	if _, err := service.TimeoutCommand(project.ID, late.ID); err != nil {
		t.Fatalf("timeout late: %v", err)
	}
	clock.Advance(6 * time.Minute)
	_, err = service.SucceedCommand(project.ID, late.ID, true)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("late correction error = %v, want ErrInvalidTransition", err)
	}
}

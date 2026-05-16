package gateway

import (
	"context"
	"testing"
	"time"
)

func TestOfflineUnlockFailsAndQueryStatusQueuesThenDispatches(t *testing.T) {
	gw := NewSimulatorGateway(ModeConfig{Heartbeat: 10 * time.Millisecond})
	service := NewService(gw, ServiceConfig{
		HeartbeatTimeout: 25 * time.Millisecond,
		OfflineScan:      5 * time.Millisecond,
		CommandTimeouts: map[string]time.Duration{
			"query_status": 80 * time.Millisecond,
		},
	})
	defer service.Stop()

	if err := service.SetMode(ModeConfig{Mode: ModeOffline}); err != nil {
		t.Fatalf("set offline mode: %v", err)
	}
	waitFor(t, 150*time.Millisecond, func() bool {
		return !service.Snapshot().Online
	})

	unlock, err := service.CreateCommand(context.Background(), "unlock", nil)
	if err != nil {
		t.Fatalf("create unlock: %v", err)
	}
	if unlock.Status != StatusFailed || unlock.Reason != "device_offline" {
		t.Fatalf("unlock = status %q reason %q, want failed device_offline", unlock.Status, unlock.Reason)
	}

	query, err := service.CreateCommand(context.Background(), "query_status", nil)
	if err != nil {
		t.Fatalf("create query_status: %v", err)
	}
	if query.Status != StatusOffline {
		t.Fatalf("query_status while offline = %q, want offline", query.Status)
	}

	if err := service.SetMode(ModeConfig{Mode: ModeNormal, Heartbeat: 10 * time.Millisecond}); err != nil {
		t.Fatalf("set normal mode: %v", err)
	}
	waitFor(t, 150*time.Millisecond, func() bool {
		record, err := service.GetCommand(query.ID)
		return err == nil && record.Status == StatusSuccess && record.Attempts == 1
	})
}

func TestDuplicateAckIsIdempotent(t *testing.T) {
	service := newTestService(t, ModeConfig{Mode: ModeDuplicateAck}, map[string]time.Duration{
		"query_status": 120 * time.Millisecond,
	})
	defer service.Stop()

	record, err := service.CreateCommand(context.Background(), "query_status", nil)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	waitFor(t, 150*time.Millisecond, func() bool {
		record, err = service.GetCommand(record.ID)
		return err == nil && record.Status == StatusSuccess
	})
	time.Sleep(30 * time.Millisecond)

	record, err = service.GetCommand(record.ID)
	if err != nil {
		t.Fatalf("get command: %v", err)
	}
	if record.FinalEventCount != 1 {
		t.Fatalf("final event count = %d, want 1", record.FinalEventCount)
	}
}

func TestDelayModeDelaysSuccessfulResult(t *testing.T) {
	service := newTestService(t, ModeConfig{Mode: ModeDelay, Delay: 40 * time.Millisecond}, map[string]time.Duration{
		"query_status": 150 * time.Millisecond,
	})
	defer service.Stop()

	record, err := service.CreateCommand(context.Background(), "query_status", nil)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	record, err = service.GetCommand(record.ID)
	if err != nil {
		t.Fatalf("get command: %v", err)
	}
	if record.Status != StatusSent {
		t.Fatalf("early delayed command status = %q, want sent", record.Status)
	}

	waitFor(t, 150*time.Millisecond, func() bool {
		record, err = service.GetCommand(record.ID)
		return err == nil && record.Status == StatusSuccess
	})
}

func TestFailModeMarksCommandFailed(t *testing.T) {
	service := newTestService(t, ModeConfig{Mode: ModeFail}, map[string]time.Duration{
		"query_status": 120 * time.Millisecond,
	})
	defer service.Stop()

	record, err := service.CreateCommand(context.Background(), "query_status", nil)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	waitFor(t, 150*time.Millisecond, func() bool {
		record, err = service.GetCommand(record.ID)
		return err == nil && record.Status == StatusFailed
	})
	if record.Reason != "simulator_failed" {
		t.Fatalf("failed reason = %q, want simulator_failed", record.Reason)
	}
}

func TestTimeoutThenAckCorrectsLowRiskCommand(t *testing.T) {
	service := newTestService(t, ModeConfig{
		Mode:          ModeTimeoutThenAck,
		TimeoutOffset: 20 * time.Millisecond,
	}, map[string]time.Duration{
		"query_status": 30 * time.Millisecond,
	})
	defer service.Stop()

	record, err := service.CreateCommand(context.Background(), "query_status", nil)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	waitFor(t, 120*time.Millisecond, func() bool {
		record, err = service.GetCommand(record.ID)
		return err == nil && record.Status == StatusSuccess && record.Corrected
	})
}

func TestReplaceLatestCancelsOlderQueuedCommand(t *testing.T) {
	service := newTestService(t, ModeConfig{Mode: ModeOffline}, map[string]time.Duration{
		"set_config": 120 * time.Millisecond,
	})
	defer service.Stop()

	waitFor(t, 150*time.Millisecond, func() bool {
		return !service.Snapshot().Online
	})

	first, err := service.CreateCommand(context.Background(), "set_config", map[string]any{"volume": 1})
	if err != nil {
		t.Fatalf("create first set_config: %v", err)
	}
	second, err := service.CreateCommand(context.Background(), "set_config", map[string]any{"volume": 2})
	if err != nil {
		t.Fatalf("create second set_config: %v", err)
	}

	first, err = service.GetCommand(first.ID)
	if err != nil {
		t.Fatalf("get first command: %v", err)
	}
	if first.Status != StatusCancelled || first.Reason != "replaced_by_latest" {
		t.Fatalf("first status = %s/%s, want cancelled/replaced_by_latest", first.Status, first.Reason)
	}
	if second.Status != StatusOffline {
		t.Fatalf("second status = %s, want offline", second.Status)
	}

	if err := service.SetMode(ModeConfig{Mode: ModeNormal, Heartbeat: 10 * time.Millisecond}); err != nil {
		t.Fatalf("set normal mode: %v", err)
	}
	waitFor(t, 150*time.Millisecond, func() bool {
		record, err := service.GetCommand(second.ID)
		return err == nil && record.Status == StatusSuccess && record.Attempts == 1
	})
}

func TestModeSwitchingWithoutRestart(t *testing.T) {
	gw := NewSimulatorGateway(ModeConfig{Mode: ModeNormal})
	service := NewService(gw, ServiceConfig{})
	defer service.Stop()

	if service.Snapshot().Mode != ModeNormal {
		t.Fatalf("initial mode = %q, want normal", service.Snapshot().Mode)
	}
	if err := service.SetMode(ModeConfig{Mode: ModeFail}); err != nil {
		t.Fatalf("set fail mode: %v", err)
	}
	if service.Snapshot().Mode != ModeFail {
		t.Fatalf("switched mode = %q, want fail", service.Snapshot().Mode)
	}
}

func newTestService(t *testing.T, gatewayConfig ModeConfig, timeouts map[string]time.Duration) *Service {
	t.Helper()
	if gatewayConfig.Heartbeat == 0 {
		gatewayConfig.Heartbeat = 10 * time.Millisecond
	}
	gw := NewSimulatorGateway(gatewayConfig)
	service := NewService(gw, ServiceConfig{
		HeartbeatTimeout: 50 * time.Millisecond,
		OfflineScan:      5 * time.Millisecond,
		CommandTimeouts:  timeouts,
	})
	return service
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition was not met within %s", timeout)
}

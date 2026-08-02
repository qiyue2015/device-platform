//go:build integration

package deviceservice_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/simulator"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	projectOneID        = "10000000-0000-0000-0000-000000000001"
	projectTwoID        = "10000000-0000-0000-0000-000000000002"
	deviceServiceUserID = "70000000-0000-0000-0000-000000000001"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func TestDeviceServicePostgresLifecycleScopeAndState(t *testing.T) {
	withDeviceServiceDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		seedProjects(t, ctx, store)
		service := newService(t, store)
		metadata := deviceservice.RequestMetadata{
			ActorType: domain.ActorTypeUser, ActorUserID: deviceServiceUserID, IPAddress: "2001:db8::10", RequestID: "request-1",
		}
		superAdminScope := deviceservice.HumanScope(deviceServiceUserID, true)
		projectMetadata := deviceservice.RequestMetadata{ActorType: domain.ActorTypeProject, ActorID: projectOneID, RequestID: "request-open"}
		if _, err := service.Create(ctx, deviceservice.ProjectScope(projectOneID), deviceservice.CreateRequest{
			Name: "Forbidden Open Create", DeviceTypeCode: domain.DeviceTypeSmartLock, ProviderCode: domain.ProviderCodeSimulator,
			ProviderProfile: domain.ProviderProfileSimulatorV1,
		}, projectMetadata); !errors.Is(err, deviceservice.ErrInvalidRequest) {
			t.Fatalf("Open Device create error = %v", err)
		}

		deviceTypes, err := service.ListDeviceTypes(ctx)
		if err != nil || len(deviceTypes) != 1 || deviceTypes[0].Code != domain.DeviceTypeSmartLock || deviceTypes[0].Revision != 2 || len(deviceTypes[0].Actions) != 3 {
			t.Fatalf("Device Types = %+v, %v", deviceTypes, err)
		}
		if deviceTypes[0].Actions[0].DispatchDeadlineMS != 30000 || deviceTypes[0].Actions[0].ProviderRequestTimeoutMS != 10000 || deviceTypes[0].Actions[0].ResultObservationTimeoutMS != 60000 {
			t.Fatalf("Device Type deadlines = %+v", deviceTypes[0].Actions[0])
		}

		wwtiotID := "LOCK-001"
		created, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: projectOneID, Name: "  Front Lock  ", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
			ProviderDeviceID: &wwtiotID,
		}, metadata)
		if err != nil {
			t.Fatal(err)
		}
		if created.Name != "Front Lock" || created.ConnectionStatus != domain.ConnectionStatusUnknown || created.LifecycleStatus != domain.LifecycleStatusActive || created.CurrentState != nil || created.LastSeenAt != nil {
			t.Fatalf("created Device = %+v", created)
		}
		var rawEnvelope []byte
		if err := db.QueryRow(`
			SELECT wd.raw_body
			FROM webhook_deliveries wd
			JOIN device_events de ON de.id = wd.event_id
			WHERE de.device_id = $1 AND de.event_type = 'device.created'
		`, created.ID).Scan(&rawEnvelope); err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(rawEnvelope, &envelope); err != nil {
			t.Fatalf("decode Delivery envelope: %v", err)
		}
		for _, field := range []string{"event_id", "schema_version", "event_type", "project_id", "device_id", "command_id", "occurred_at", "source", "payload"} {
			if _, exists := envelope[field]; !exists {
				t.Fatalf("Delivery envelope lacks %s: %s", field, rawEnvelope)
			}
		}
		if envelope["schema_version"] != float64(1) || envelope["event_type"] != "device.created" || envelope["source"] != "admin" || envelope["command_id"] != nil {
			t.Fatalf("Delivery envelope = %s", rawEnvelope)
		}
		simulator, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: projectOneID, Name: "Simulator", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeSimulator, ProviderProfile: domain.ProviderProfileSimulatorV1,
		}, metadata)
		if err != nil || simulator.ProviderDeviceID != simulator.ID || simulator.AccessType != domain.AccessTypeSimulator || simulator.Adapter != domain.AdapterSimulator {
			t.Fatalf("simulator Device = %+v, %v", simulator, err)
		}

		if _, err := service.Get(ctx, deviceservice.ProjectScope(projectTwoID), created.ID); !errors.Is(err, deviceservice.ErrDeviceNotFound) {
			t.Fatalf("cross-Project get error = %v", err)
		}
		openRename := "Forbidden Open Rename"
		if _, err := service.Update(ctx, deviceservice.ProjectScope(projectOneID), created.ID, deviceservice.UpdateRequest{Name: &openRename}, projectMetadata); !errors.Is(err, deviceservice.ErrInvalidRequest) {
			t.Fatalf("Open Device update error = %v", err)
		}
		if _, err := service.List(ctx, deviceservice.ProjectScope(projectOneID), deviceservice.ListRequest{ProjectID: stringPointer(projectTwoID)}); !errors.Is(err, deviceservice.ErrInvalidRequest) {
			t.Fatalf("Open Project override error = %v", err)
		}
		providerCode := domain.ProviderCodeWWTIOT
		listed, err := service.List(ctx, deviceservice.ProjectScope(projectOneID), deviceservice.ListRequest{ProviderCode: &providerCode, PageSize: 1})
		if err != nil || listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].ID != created.ID || listed.Page != 1 || listed.PageSize != 1 {
			t.Fatalf("filtered list = %+v, %v", listed, err)
		}
		all, err := service.List(ctx, superAdminScope, deviceservice.ListRequest{Page: 1, PageSize: 2})
		if err != nil || all.Total != 2 || len(all.Items) != 2 {
			t.Fatalf("admin list = %+v, %v", all, err)
		}
		wantIDs := []string{created.ID, simulator.ID}
		sort.Sort(sort.Reverse(sort.StringSlice(wantIDs)))
		if all.Items[0].ID != wantIDs[0] || all.Items[1].ID != wantIDs[1] {
			t.Fatalf("stable order = %s, %s; want %v", all.Items[0].ID, all.Items[1].ID, wantIDs)
		}
		defaults, err := service.List(ctx, superAdminScope, deviceservice.ListRequest{})
		if err != nil || defaults.Page != 1 || defaults.PageSize != 20 || defaults.Total != 2 {
			t.Fatalf("default pagination = %+v, %v", defaults, err)
		}

		insertVerifiedState(t, db, created, time.Date(2026, 7, 31, 16, 1, 0, 0, time.UTC))
		withState, err := service.Get(ctx, deviceservice.ProjectScope(projectOneID), created.ID)
		if err != nil || withState.CurrentState == nil || withState.CurrentState.State["lock_state"] != "locked" ||
			withState.CurrentState.EvidenceStatus != domain.EvidenceVerified || withState.CurrentState.ObservedAt.IsZero() ||
			withState.LastSeenAt == nil {
			t.Fatalf("trusted current state = %+v, %v", withState, err)
		}

		newName := "Renamed Lock"
		var eventsBeforeRename, deliveriesBeforeRename int
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE device_id = $1`, created.ID).Scan(&eventsBeforeRename); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries WHERE project_id = $1`, projectOneID).Scan(&deliveriesBeforeRename); err != nil {
			t.Fatal(err)
		}
		updated, err := service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{Name: &newName}, metadata)
		if err != nil || updated.Name != newName {
			t.Fatalf("rename = %+v, %v", updated, err)
		}
		var eventsAfterRename, deliveriesAfterRename int
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE device_id = $1`, created.ID).Scan(&eventsAfterRename); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries WHERE project_id = $1`, projectOneID).Scan(&deliveriesAfterRename); err != nil || eventsAfterRename != eventsBeforeRename || deliveriesAfterRename != deliveriesBeforeRename {
			t.Fatalf("name-only update created Event/Delivery: events %d->%d deliveries %d->%d err=%v", eventsBeforeRename, eventsAfterRename, deliveriesBeforeRename, deliveriesAfterRename, err)
		}
		var auditsBeforeNoop int
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE resource_id = $1`, created.ID).Scan(&auditsBeforeNoop); err != nil {
			t.Fatal(err)
		}
		if noOp, err := service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{Name: &newName}, metadata); err != nil || noOp.Name != newName {
			t.Fatalf("name no-op = %+v, %v", noOp, err)
		}
		var auditsAfterNoop int
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE resource_id = $1`, created.ID).Scan(&auditsAfterNoop); err != nil || auditsAfterNoop != auditsBeforeNoop {
			t.Fatalf("name no-op created Audit: before=%d after=%d err=%v", auditsBeforeNoop, auditsAfterNoop, err)
		}
		disabled := domain.LifecycleStatusDisabled
		updated, err = service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{LifecycleStatus: &disabled}, metadata)
		if err != nil || updated.LifecycleStatus != disabled {
			t.Fatalf("disable = %+v, %v", updated, err)
		}
		active := domain.LifecycleStatusActive
		updated, err = service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{LifecycleStatus: &active}, metadata)
		if err != nil || updated.LifecycleStatus != active {
			t.Fatalf("reactivate = %+v, %v", updated, err)
		}
		deleted := domain.LifecycleStatusDeleted
		if _, err := service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{Name: &newName, LifecycleStatus: &deleted}, metadata); !errors.Is(err, deviceservice.ErrInvalidRequest) {
			t.Fatalf("combined name/delete error = %v", err)
		}
		updated, err = service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{LifecycleStatus: &deleted}, metadata)
		if err != nil || updated.LifecycleStatus != deleted {
			t.Fatalf("delete = %+v, %v", updated, err)
		}
		if _, err := service.Update(ctx, superAdminScope, created.ID, deviceservice.UpdateRequest{Name: &newName}, metadata); !errors.Is(err, deviceservice.ErrDeviceImmutable) {
			t.Fatalf("deleted rename error = %v", err)
		}
		if historical, err := service.Get(ctx, deviceservice.ProjectScope(projectOneID), created.ID); err != nil || historical.LifecycleStatus != deleted {
			t.Fatalf("deleted historical read = %+v, %v", historical, err)
		}

		_, err = service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: projectTwoID, Name: "Replacement", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
			ProviderDeviceID: &wwtiotID,
		}, metadata)
		if !errors.Is(err, deviceservice.ErrProviderDeviceConflict) {
			t.Fatalf("deleted Provider identity was reused: %v", err)
		}
		var unconfiguredDeliveries int
		if err := db.QueryRow(`SELECT count(*) FROM webhook_deliveries WHERE project_id = $1`, projectTwoID).Scan(&unconfiguredDeliveries); err != nil || unconfiguredDeliveries != 0 {
			t.Fatalf("unconfigured Project deliveries = %d, %v", unconfiguredDeliveries, err)
		}
		var badReasonCount int
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE event_type = 'device.lifecycle_changed' AND payload->>'reason_code' <> 'admin_requested'`).Scan(&badReasonCount); err != nil || badReasonCount != 0 {
			t.Fatalf("lifecycle reason count = %d, %v", badReasonCount, err)
		}
		var leaked int
		if err := db.QueryRow(`
			SELECT
				(SELECT count(*) FROM audit_logs WHERE metadata::text LIKE '%top-secret-key%' OR metadata::text LIKE '%gps.example.test%') +
				(SELECT count(*) FROM webhook_deliveries WHERE convert_from(raw_body, 'UTF8') LIKE '%top-secret-key%' OR convert_from(raw_body, 'UTF8') LIKE '%gps.example.test%')
		`).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatalf("Provider configuration leaked into durable output: count=%d err=%v", leaked, err)
		}
	})
}

func TestDeviceServicePostgresConflictConcurrencyAndRollback(t *testing.T) {
	withDeviceServiceDatabase(t, func(db *sql.DB) {
		ctx := context.Background()
		store := repository.NewPostgresStore(db)
		seedProjects(t, ctx, store)
		service := newService(t, store)
		metadata := deviceservice.RequestMetadata{ActorType: domain.ActorTypeUser, ActorUserID: deviceServiceUserID, RequestID: "request-rollback"}
		superAdminScope := deviceservice.HumanScope(deviceServiceUserID, true)
		providerID := "CONCURRENT-LOCK"

		results := make(chan error, 2)
		var wait sync.WaitGroup
		for _, projectID := range []string{projectOneID, projectTwoID} {
			wait.Add(1)
			go func(projectID string) {
				defer wait.Done()
				_, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
					ProjectID: projectID, Name: projectID, DeviceTypeCode: domain.DeviceTypeSmartLock,
					ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
					ProviderDeviceID: &providerID,
				}, metadata)
				results <- err
			}(projectID)
		}
		wait.Wait()
		close(results)
		var successes, conflicts int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, deviceservice.ErrProviderDeviceConflict):
				conflicts++
			default:
				t.Fatalf("concurrent create error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent create successes=%d conflicts=%d", successes, conflicts)
		}

		var deviceID string
		if err := db.QueryRow(`SELECT id::text FROM devices WHERE provider_device_id = $1`, providerID).Scan(&deviceID); err != nil {
			t.Fatal(err)
		}
		disabled := domain.LifecycleStatusDisabled
		results = make(chan error, 2)
		for index := 0; index < 2; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, err := service.Update(ctx, superAdminScope, deviceID, deviceservice.UpdateRequest{LifecycleStatus: &disabled}, metadata)
				results <- err
			}()
		}
		wait.Wait()
		close(results)
		successes, conflicts = 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, deviceservice.ErrLifecycleTransition) {
				conflicts++
			} else {
				t.Fatalf("concurrent lifecycle error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent lifecycle successes=%d rejected=%d", successes, conflicts)
		}

		if _, err := db.Exec(`ALTER TABLE webhook_deliveries ADD CONSTRAINT reject_device_delivery_test CHECK (status <> 'pending') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		var devicesBefore int
		if err := db.QueryRow(`SELECT count(*) FROM devices`).Scan(&devicesBefore); err != nil {
			t.Fatal(err)
		}
		rollbackProviderID := "ROLLBACK-DELIVERY"
		if _, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: projectOneID, Name: "Must Roll Back", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
			ProviderDeviceID: &rollbackProviderID,
		}, metadata); err == nil {
			t.Fatal("Device create committed without configured Webhook Delivery")
		}
		var devicesAfter int
		if err := db.QueryRow(`SELECT count(*) FROM devices`).Scan(&devicesAfter); err != nil || devicesAfter != devicesBefore {
			t.Fatalf("Device did not roll back with Delivery: before=%d after=%d err=%v", devicesBefore, devicesAfter, err)
		}
		if _, err := db.Exec(`ALTER TABLE webhook_deliveries DROP CONSTRAINT reject_device_delivery_test`); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_device_create_audit_test CHECK (action <> 'device.created') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		rollbackAuditProviderID := "ROLLBACK-AUDIT"
		if _, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: projectOneID, Name: "Must Roll Back Audit", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
			ProviderDeviceID: &rollbackAuditProviderID,
		}, metadata); err == nil {
			t.Fatal("Device create committed without Audit")
		}
		var rolledBackAggregate int
		if err := db.QueryRow(`
			SELECT
				(SELECT count(*) FROM devices WHERE provider_device_id = $1) +
				(SELECT count(*) FROM device_events WHERE payload::text LIKE '%' || $1 || '%')
		`, rollbackAuditProviderID).Scan(&rolledBackAggregate); err != nil || rolledBackAggregate != 0 {
			t.Fatalf("Device/Event did not roll back with Audit: count=%d err=%v", rolledBackAggregate, err)
		}
		if _, err := db.Exec(`ALTER TABLE audit_logs DROP CONSTRAINT reject_device_create_audit_test`); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_device_update_test CHECK (action <> 'device.updated') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		before, err := service.Get(ctx, superAdminScope, deviceID)
		if err != nil {
			t.Fatal(err)
		}
		rolledBackName := "Must Roll Back"
		if _, err := service.Update(ctx, superAdminScope, deviceID, deviceservice.UpdateRequest{Name: &rolledBackName}, metadata); err == nil {
			t.Fatal("rename committed without Audit")
		}
		after, err := service.Get(ctx, superAdminScope, deviceID)
		if err != nil || after.Name != before.Name {
			t.Fatalf("rename rollback = %+v, %v", after, err)
		}
		if _, err := db.Exec(`ALTER TABLE audit_logs DROP CONSTRAINT reject_device_update_test`); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`ALTER TABLE device_events ADD CONSTRAINT reject_device_lifecycle_test CHECK (event_type <> 'device.lifecycle_changed') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		active := domain.LifecycleStatusActive
		if _, err := service.Update(ctx, superAdminScope, deviceID, deviceservice.UpdateRequest{LifecycleStatus: &active}, metadata); err == nil {
			t.Fatal("lifecycle committed without Event")
		}
		after, err = service.Get(ctx, superAdminScope, deviceID)
		if err != nil || after.LifecycleStatus != domain.LifecycleStatusDisabled {
			t.Fatalf("lifecycle rollback = %+v, %v", after, err)
		}
		if _, err := db.Exec(`ALTER TABLE device_events DROP CONSTRAINT reject_device_lifecycle_test`); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`ALTER TABLE audit_logs ADD CONSTRAINT reject_device_lifecycle_audit_test CHECK (action <> 'device.lifecycle_changed') NOT VALID`); err != nil {
			t.Fatal(err)
		}
		var eventsBefore int
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE device_id = $1`, deviceID).Scan(&eventsBefore); err != nil {
			t.Fatal(err)
		}
		deleted := domain.LifecycleStatusDeleted
		if _, err := service.Update(ctx, superAdminScope, deviceID, deviceservice.UpdateRequest{LifecycleStatus: &deleted}, metadata); err == nil {
			t.Fatal("lifecycle committed without Audit")
		}
		var eventsAfter int
		if err := db.QueryRow(`SELECT count(*) FROM device_events WHERE device_id = $1`, deviceID).Scan(&eventsAfter); err != nil || eventsAfter != eventsBefore {
			t.Fatalf("Event did not roll back with Audit: before=%d after=%d err=%v", eventsBefore, eventsAfter, err)
		}
		conflictingProject := projectOneID
		if before.ProjectID == projectOneID {
			conflictingProject = projectTwoID
		}
		if _, err := service.Create(ctx, superAdminScope, deviceservice.CreateRequest{
			ProjectID: conflictingProject, Name: "Still Conflicts", DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeWWTIOT, ProviderProfile: domain.ProviderProfileWWTIOTV2,
			ProviderDeviceID: &providerID,
		}, metadata); !errors.Is(err, deviceservice.ErrProviderDeviceConflict) {
			t.Fatalf("rolled-back delete released Provider identity: %v", err)
		}
	})
}

func newService(t *testing.T, store repository.DeviceStore) *deviceservice.Service {
	t.Helper()
	allActions := map[domain.ActionIdentifier]domain.ProviderActionAvailability{
		domain.ActionIdentifier("unlock"):       domain.ProviderActionSupported,
		domain.ActionIdentifier("lock"):         domain.ProviderActionSupported,
		domain.ActionIdentifier("query_status"): domain.ProviderActionSupported,
	}
	service, err := deviceservice.New(store, deviceservice.Config{
		Providers: []deviceservice.ProviderRegistration{
			{
				Provider: deviceservice.Provider{
					Code: domain.ProviderCodeSimulator, AccessType: domain.AccessTypeSimulator,
					TransportProtocol: domain.TransportProtocolInternal, Adapter: domain.AdapterSimulator,
					Profiles: []string{domain.ProviderProfileSimulatorV1}, IntegrationStatus: domain.ProviderIntegrationVerified,
					ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{domain.ProviderProfileSimulatorV1: allActions},
				},
				IdentityPolicy: deviceservice.DeviceIdentityPolicyFunc(simulator.NormalizeDeviceIdentity),
			},
			{
				Provider: deviceservice.Provider{
					Code: domain.ProviderCodeWWTIOT, AccessType: domain.AccessTypeCloudAPI,
					TransportProtocol: domain.TransportProtocolHTTP, Adapter: domain.AdapterWWTIOTCloudAPI,
					Profiles: []string{domain.ProviderProfileWWTIOTV2}, IntegrationStatus: domain.ProviderIntegrationConfiguredUnverified,
					ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{domain.ProviderProfileWWTIOTV2: allActions},
				},
				IdentityPolicy: deviceservice.DeviceIdentityPolicyFunc(wwtiot.NormalizeDeviceIdentity),
			},
		},
		Random: rand.Reader, Clock: fixedClock{now: time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func seedProjects(t *testing.T, ctx context.Context, store *repository.PostgresStore) {
	t.Helper()
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		if err := tx.Users().Create(ctx, domain.User{
			ID: deviceServiceUserID, Email: "admin@example.test", PasswordHash: "hash", DisplayName: "Test Admin",
			IsSuperAdmin: true, Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		for index, projectID := range []string{projectOneID, projectTwoID} {
			if err := tx.Projects().Create(ctx, domain.Project{
				ID: projectID, Name: fmt.Sprintf("Project %d", index+1), APIKeyDigest: bytes.Repeat([]byte{byte(index + 1)}, 32),
				ManagerUserID: deviceServiceUserID, IPWhitelist: []string{}, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
			ProjectID: projectOneID, Version: 1, Ciphertext: bytes.Repeat([]byte{0x33}, 33),
			Nonce: bytes.Repeat([]byte{0x44}, 12), EncryptionKeyVersion: 1, CreatedAt: now,
		}); err != nil {
			return err
		}
		webhookURL := "https://hooks.example.test/device-events"
		secretVersion := 1
		return tx.Projects().SetWebhookConfiguration(ctx, projectOneID, &webhookURL, 1, &secretVersion)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func insertVerifiedState(t *testing.T, db *sql.DB, device deviceservice.Device, observedAt time.Time) {
	t.Helper()
	rawMessageID := "40000000-0000-0000-0000-000000000001"
	if _, err := db.Exec(`
		INSERT INTO device_raw_messages (
			id, device_id, provider_code, provider_profile, provider_device_id, access_type, transport_protocol,
			adapter, evidence_status, direction, headers, body, deduplication_key, received_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'verified', 'inbound', '{}'::jsonb, $9, 'state-1', $10, $10)
	`, rawMessageID, device.ID, device.ProviderCode, device.ProviderProfile, device.ProviderDeviceID,
		device.AccessType, device.TransportProtocol, device.Adapter, []byte(`{"lockstatus":0}`), observedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO device_states (id, device_id, state, evidence_status, reported_at, observed_at, raw_message_id, created_at)
		VALUES ('50000000-0000-0000-0000-000000000001', $1, '{"lock_state":"locked"}', 'verified', NULL, $2, $3, $2)
	`, device.ID, observedAt, rawMessageID); err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string { return &value }

func withDeviceServiceDatabase(t *testing.T, fn func(*sql.DB)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimPrefix(parsedBase.Path, "/"), "_test") {
		t.Fatalf("refusing Device service integration test against database %q", strings.TrimPrefix(parsedBase.Path, "/"))
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("device_service_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop Device service test schema: %v", err)
		}
	}()
	query := parsedBase.Query()
	query.Set("search_path", schema)
	parsedBase.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsedBase.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	fn(db)
}

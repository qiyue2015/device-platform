//go:build integration

package omni

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	inboundProjectID      = "a1000000-0000-4000-8000-000000000001"
	inboundOtherProjectID = "a1000000-0000-4000-8000-000000000002"
	inboundOmniDeviceID   = "a2000000-0000-4000-8000-000000000001"
	inboundShadowDeviceID = "a2000000-0000-4000-8000-000000000002"
	inboundOtherDeviceID  = "a2000000-0000-4000-8000-000000000003"
	inboundOmniIMEI       = "123456789012345"
	inboundOtherIMEI      = "123456789012346"
)

type inboundSequenceReader struct {
	mu   sync.Mutex
	next byte
}

func (r *inboundSequenceReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range buffer {
		r.next++
		buffer[index] = r.next
	}
	return len(buffer), nil
}

func TestPersistentInboundRecorderPreservesDuplicateAndIsolationFacts(t *testing.T) {
	withOmniDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		seedInboundDevices(t, store)
		recorder, err := NewPersistentInboundRecorder(store, InboundRecorderConfig{
			Random: &inboundSequenceReader{},
			Clock:  fixedClock{now: time.Date(2026, time.August, 1, 11, 22, 33, 0, time.UTC)},
		})
		if err != nil {
			t.Fatal(err)
		}

		bikeQ0 := decodedInboundFrame(t, domain.ProviderProfileOmniBikeV207,
			[]byte("*CMDR,OM,"+inboundOmniIMEI+",260801112233,Q0,412#\n"))
		firstRequest := InboundRecordRequest{
			Profile: domain.ProviderProfileOmniBikeV207, ConnectionID: "connection-generation-one",
			FirstFrame: true, Decoded: bikeQ0,
		}
		first, err := recorder.Record(context.Background(), firstRequest)
		if err != nil || !first.Accepted || first.Duplicate || first.Device == nil || first.Device.ID != inboundOmniDeviceID {
			t.Fatalf("first Omni record=%+v err=%v", first, err)
		}
		duplicateRequest := firstRequest
		duplicate, err := recorder.Record(context.Background(), duplicateRequest)
		if err != nil || !duplicate.Accepted || !duplicate.Duplicate || duplicate.RawMessageID != first.RawMessageID {
			t.Fatalf("duplicate Omni record=%+v err=%v", duplicate, err)
		}
		independentRequest := firstRequest
		independentRequest.ConnectionID = "connection-generation-two"
		independent, err := recorder.Record(context.Background(), independentRequest)
		if err != nil || !independent.Accepted || independent.Duplicate || independent.RawMessageID == first.RawMessageID {
			t.Fatalf("independent Omni observation=%+v err=%v", independent, err)
		}
		statusRequest := InboundRecordRequest{
			Profile: domain.ProviderProfileOmniBikeV207, ConnectionID: "connection-generation-one",
			ExpectedDeviceID: inboundOmniDeviceID,
			Decoded: decodedInboundFrame(t, domain.ProviderProfileOmniBikeV207,
				[]byte("*CMDR,OM,"+inboundOmniIMEI+",260801112234,S5,412,30,5,0,0#\n")),
		}
		status, err := recorder.Record(context.Background(), statusRequest)
		if err != nil || !status.Accepted || status.Duplicate || status.Device == nil || status.Device.ID != inboundOmniDeviceID {
			t.Fatalf("Omni status record=%+v err=%v", status, err)
		}

		var message domain.RawMessage
		if err := store.TransactProviderMessage(context.Background(), func(tx repository.ProviderMessageTx) error {
			var lookupErr error
			message, lookupErr = tx.Messages().GetByDeduplicationKey(
				context.Background(), domain.ProviderCodeOmni, inboundDeduplicationKey(firstRequest),
			)
			return lookupErr
		}); err != nil {
			t.Fatal(err)
		}
		if message.DeviceID == nil || *message.DeviceID != inboundOmniDeviceID ||
			message.ProviderProfile != domain.ProviderProfileOmniBikeV207 || message.EvidenceStatus != domain.EvidenceUnverified {
			t.Fatalf("stored Omni RawMessage=%+v", message)
		}
		if bytes.Contains(message.Body, []byte(inboundOmniIMEI)) || bytes.Contains(message.Body, bikeQ0.Raw) {
			t.Fatal("RawMessage diagnostic body retained identity or raw frame")
		}
		var summary map[string]any
		if err := json.Unmarshal(message.Body, &summary); err != nil || summary["command"] != "Q0" || summary["evidence_status"] != "unverified" {
			t.Fatalf("RawMessage summary=%+v err=%v", summary, err)
		}

		action := "provider.message_received"
		audits, total, err := store.Audits().List(context.Background(), repository.ListAuditsRequest{
			ProjectID: stringPointer(inboundProjectID), Action: &action, Limit: 100,
		})
		if err != nil || total != 4 || len(audits) != 4 {
			t.Fatalf("accepted Omni Audits=%+v total=%d err=%v", audits, total, err)
		}
		duplicateSeen := false
		independentSeen := false
		for _, audit := range audits {
			if audit.ActorID == nil || *audit.ActorID != domain.ProviderCodeOmni || audit.ProjectID == nil ||
				*audit.ProjectID != inboundProjectID || audit.ResourceID == nil {
				t.Fatalf("accepted Omni Audit=%+v", audit)
			}
			command, _ := audit.Metadata["command"].(string)
			switch command {
			case "Q0":
				if *audit.ResourceID != first.RawMessageID && *audit.ResourceID != independent.RawMessageID {
					t.Fatalf("Q0 Omni Audit=%+v", audit)
				}
			case "S5":
				if *audit.ResourceID != status.RawMessageID {
					t.Fatalf("S5 Omni Audit=%+v", audit)
				}
			default:
				t.Fatalf("unexpected accepted Omni Audit command=%q audit=%+v", command, audit)
			}
			if command == "Q0" {
				value, _ := audit.Metadata["duplicate"].(bool)
				if value {
					duplicateSeen = true
				} else if *audit.ResourceID == independent.RawMessageID {
					independentSeen = true
				}
			}
		}
		if !duplicateSeen || !independentSeen {
			t.Fatalf("Omni duplicate/independent observations missing: duplicate=%v independent=%v", duplicateSeen, independentSeen)
		}

		unmatchedRequest := InboundRecordRequest{
			Profile: domain.ProviderProfileOmniIoTV135, ConnectionID: "unmatched-provider-identity", FirstFrame: true,
			Decoded: decodedInboundFrame(t, domain.ProviderProfileOmniIoTV135,
				[]byte("*SCOR,OM,"+inboundOtherIMEI+",Q0,412,80,28#\n")),
		}
		unmatched, err := recorder.Record(context.Background(), unmatchedRequest)
		if err != nil || unmatched.Accepted || unmatched.Device != nil || unmatched.RejectCode != "device_not_registered" {
			t.Fatalf("cross-Provider Omni record=%+v err=%v", unmatched, err)
		}
		profileMismatchRequest := InboundRecordRequest{
			Profile: domain.ProviderProfileOmniIoTV135, ConnectionID: "profile-mismatch", FirstFrame: true,
			Decoded: decodedInboundFrame(t, domain.ProviderProfileOmniIoTV135,
				[]byte("*SCOR,OM,"+inboundOmniIMEI+",Q0,412,80,28#\n")),
		}
		profileMismatch, err := recorder.Record(context.Background(), profileMismatchRequest)
		if err != nil || profileMismatch.Accepted || profileMismatch.Device != nil || profileMismatch.RejectCode != "device_binding_mismatch" {
			t.Fatalf("cross-profile Omni record=%+v err=%v", profileMismatch, err)
		}

		for _, request := range []InboundRecordRequest{unmatchedRequest, profileMismatchRequest} {
			if err := store.TransactProviderMessage(context.Background(), func(tx repository.ProviderMessageTx) error {
				stored, lookupErr := tx.Messages().GetByDeduplicationKey(
					context.Background(), domain.ProviderCodeOmni, inboundDeduplicationKey(request),
				)
				if lookupErr != nil {
					return lookupErr
				}
				if stored.DeviceID != nil || stored.EvidenceStatus != domain.EvidenceUnverified {
					return fmt.Errorf("unmatched RawMessage gained Device evidence: %+v", stored)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		rejectedAction := "provider.message_rejected"
		rejected, total, err := store.Audits().List(context.Background(), repository.ListAuditsRequest{
			Action: &rejectedAction, Limit: 100,
		})
		if err != nil || total != 2 || len(rejected) != 2 {
			t.Fatalf("rejected Omni Audits=%+v total=%d err=%v", rejected, total, err)
		}
		for _, audit := range rejected {
			if audit.ProjectID != nil || audit.ActorID == nil || *audit.ActorID != domain.ProviderCodeOmni {
				t.Fatalf("unmatched Omni Audit crossed Project=%+v", audit)
			}
		}
		projectTwoAudits, total, err := store.Audits().List(context.Background(), repository.ListAuditsRequest{
			ProjectID: stringPointer(inboundOtherProjectID), Limit: 100,
		})
		if err != nil || total != 0 || len(projectTwoAudits) != 0 {
			t.Fatalf("Omni message crossed to WWTIOT Project: audits=%+v total=%d err=%v", projectTwoAudits, total, err)
		}
		if _, err := store.Devices().GetCurrentState(context.Background(), inboundOmniDeviceID); err != sql.ErrNoRows {
			t.Fatalf("unverified Omni input created DeviceState: %v", err)
		}
		device, err := store.Devices().Get(context.Background(), inboundOmniDeviceID)
		if err != nil || device.ConnectionStatus != domain.ConnectionStatusUnknown {
			t.Fatalf("unverified Omni input changed connection state: device=%+v err=%v", device, err)
		}
		assertOmniTableCount(t, db, "device_command_results", 0)
	})
}

func TestPersistentInboundRecorderRollsBackRawMessageWhenAuditFails(t *testing.T) {
	withOmniDatabase(t, func(db *sql.DB, store *repository.PostgresStore) {
		seedInboundDevices(t, store)
		if _, err := db.Exec(`
			CREATE FUNCTION fail_omni_audit() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'injected Omni Audit failure'; END $$;
			CREATE TRIGGER fail_omni_audit BEFORE INSERT ON audit_logs
				FOR EACH ROW EXECUTE FUNCTION fail_omni_audit()
		`); err != nil {
			t.Fatal(err)
		}
		recorder, err := NewPersistentInboundRecorder(store, InboundRecorderConfig{Random: &inboundSequenceReader{}})
		if err != nil {
			t.Fatal(err)
		}
		request := InboundRecordRequest{
			Profile: domain.ProviderProfileOmniBikeV207, ConnectionID: "rollback-connection", FirstFrame: true,
			Decoded: decodedInboundFrame(t, domain.ProviderProfileOmniBikeV207,
				[]byte("*CMDR,OM,"+inboundOmniIMEI+",260801112233,Q0,412#\n")),
		}
		if _, err := recorder.Record(context.Background(), request); err == nil {
			t.Fatal("Audit failure did not fail Omni receive transaction")
		}
		assertOmniTableCount(t, db, "device_raw_messages", 0)
		assertOmniTableCount(t, db, "audit_logs", 0)
	})
}

func decodedInboundFrame(t *testing.T, profile string, raw []byte) DecodedFrame {
	t.Helper()
	frame, err := ParseFrame(profile, raw)
	if err != nil {
		t.Fatal(err)
	}
	return DecodedFrame{Raw: append([]byte(nil), raw...), Frame: frame}
}

func seedInboundDevices(t *testing.T, store *repository.PostgresStore) {
	t.Helper()
	ctx := context.Background()
	deviceType, err := store.DeviceTypes().GetByCode(ctx, domain.DeviceTypeSmartLock)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	if err := store.WithinTransaction(ctx, func(tx *repository.PostgresTx) error {
		for _, project := range []domain.Project{
			{ID: inboundProjectID, Name: "Omni Project", APIKeyDigest: bytes.Repeat([]byte{0xa1}, 32), IPWhitelist: []string{}, CreatedAt: now, UpdatedAt: now},
			{ID: inboundOtherProjectID, Name: "WWTIOT Project", APIKeyDigest: bytes.Repeat([]byte{0xa2}, 32), IPWhitelist: []string{}, CreatedAt: now, UpdatedAt: now},
		} {
			if err := tx.Projects().Create(ctx, project); err != nil {
				return err
			}
		}
		for _, device := range []domain.Device{
			{
				ID: inboundOmniDeviceID, ProjectID: inboundProjectID, DeviceTypeID: deviceType.ID,
				Name: "Omni Bike Lock", ProviderCode: domain.ProviderCodeOmni,
				ProviderProfile: domain.ProviderProfileOmniBikeV207, ProviderDeviceID: inboundOmniIMEI,
				AccessType: domain.AccessTypeDirectDevice, TransportProtocol: domain.TransportProtocolTCP,
				Adapter: domain.AdapterOmniDirectTCP, ConnectionStatus: domain.ConnectionStatusUnknown,
				LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: inboundShadowDeviceID, ProjectID: inboundOtherProjectID, DeviceTypeID: deviceType.ID,
				Name: "WWTIOT Same Identity Text", ProviderCode: domain.ProviderCodeWWTIOT,
				ProviderProfile: domain.ProviderProfileWWTIOTV2, ProviderDeviceID: inboundOmniIMEI,
				AccessType: domain.AccessTypeCloudAPI, TransportProtocol: domain.TransportProtocolHTTP,
				Adapter: domain.AdapterWWTIOTCloudAPI, ConnectionStatus: domain.ConnectionStatusUnknown,
				LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: inboundOtherDeviceID, ProjectID: inboundOtherProjectID, DeviceTypeID: deviceType.ID,
				Name: "WWTIOT Only Identity", ProviderCode: domain.ProviderCodeWWTIOT,
				ProviderProfile: domain.ProviderProfileWWTIOTV2, ProviderDeviceID: inboundOtherIMEI,
				AccessType: domain.AccessTypeCloudAPI, TransportProtocol: domain.TransportProtocolHTTP,
				Adapter: domain.AdapterWWTIOTCloudAPI, ConnectionStatus: domain.ConnectionStatusUnknown,
				LifecycleStatus: domain.LifecycleStatusActive, CreatedAt: now, UpdatedAt: now,
			},
		} {
			if err := tx.Devices().Create(ctx, device); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertOmniTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count=%d, want %d", table, count, want)
	}
}

func stringPointer(value string) *string { return &value }

func withOmniDatabase(t *testing.T, fn func(*sql.DB, *repository.PostgresStore)) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if baseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("omni_inbound_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop Omni schema: %v", err)
		}
	}()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	fn(db, repository.NewPostgresStore(db))
}

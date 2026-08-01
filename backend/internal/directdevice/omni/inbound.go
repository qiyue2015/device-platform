package omni

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const unresolvedProviderDeviceID = "unresolved"

type InboundRecordRequest struct {
	Profile          string
	ConnectionID     string
	FirstFrame       bool
	ExpectedDeviceID string
	Decoded          DecodedFrame
}

type InboundRecordResult struct {
	RawMessageID string
	Device       *domain.Device
	Accepted     bool
	Duplicate    bool
	RejectCode   string
}

type InboundRecorder interface {
	Record(context.Context, InboundRecordRequest) (InboundRecordResult, error)
}

type InboundRecorderConfig struct {
	Random io.Reader
	Clock  Clock
}

type PersistentInboundRecorder struct {
	store    repository.ProviderMessageStore
	random   io.Reader
	randomMu sync.Mutex
	clock    Clock
}

func NewPersistentInboundRecorder(store repository.ProviderMessageStore, config InboundRecorderConfig) (*PersistentInboundRecorder, error) {
	if store == nil {
		return nil, fmt.Errorf("omni inbound store is required")
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &PersistentInboundRecorder{store: store, random: config.Random, clock: config.Clock}, nil
}

func (r *PersistentInboundRecorder) Record(ctx context.Context, request InboundRecordRequest) (InboundRecordResult, error) {
	if r == nil || !validProfile(request.Profile) || strings.TrimSpace(request.ConnectionID) == "" {
		return InboundRecordResult{}, fmt.Errorf("invalid Omni inbound record request")
	}
	messageID, auditID, err := r.newIDs()
	if err != nil {
		return InboundRecordResult{}, err
	}
	now := r.clock.Now().UTC()
	result := InboundRecordResult{RawMessageID: messageID}
	err = r.store.TransactProviderMessage(ctx, func(tx repository.ProviderMessageTx) error {
		device, accepted, rejectCode, resolveErr := resolveInboundDevice(ctx, tx.Devices(), request)
		if resolveErr != nil {
			return resolveErr
		}
		result.Accepted = accepted
		result.RejectCode = rejectCode
		if accepted {
			result.Device = &device
		}

		providerDeviceID := unresolvedProviderDeviceID
		if request.Decoded.Err == nil && validIMEI(request.Decoded.Frame.IMEI) {
			providerDeviceID = request.Decoded.Frame.IMEI
		}
		deduplicationKey := inboundDeduplicationKey(request)
		existing, lookupErr := tx.Messages().GetByDeduplicationKey(ctx, domain.ProviderCodeOmni, deduplicationKey)
		switch {
		case lookupErr == nil:
			result.RawMessageID = existing.ID
			result.Duplicate = true
		case !errors.Is(lookupErr, sql.ErrNoRows):
			return lookupErr
		default:
			body, encodeErr := json.Marshal(inboundSafeSummary(request, rejectCode))
			if encodeErr != nil {
				return fmt.Errorf("encode Omni RawMessage summary: %w", encodeErr)
			}
			var deviceID *string
			if accepted {
				value := device.ID
				deviceID = &value
			}
			if createErr := tx.Messages().Create(ctx, domain.RawMessage{
				ID: messageID, DeviceID: deviceID, ProviderCode: domain.ProviderCodeOmni,
				ProviderProfile: request.Profile, ProviderDeviceID: providerDeviceID,
				AccessType: domain.AccessTypeDirectDevice, TransportProtocol: domain.TransportProtocolTCP,
				Adapter: domain.AdapterOmniDirectTCP, Direction: domain.RawMessageInbound,
				EvidenceStatus: domain.EvidenceUnverified, DeduplicationKey: deduplicationKey,
				Headers: map[string]any{
					"connection_id": request.ConnectionID,
					"parse_status":  inboundParseStatus(request.Decoded.Err),
				},
				Body: body, ReceivedAt: now, CreatedAt: now,
			}); createErr != nil {
				return createErr
			}
		}

		action := "provider.message_rejected"
		auditResult := domain.AuditResultFailure
		if accepted {
			action = "provider.message_received"
			auditResult = domain.AuditResultSuccess
		}
		var projectID *string
		if accepted {
			value := device.ProjectID
			projectID = &value
		}
		resourceID := result.RawMessageID
		actorID := domain.ProviderCodeOmni
		return tx.Audits().Create(ctx, domain.AuditLog{
			ID: auditID, ActorType: domain.ActorTypeProvider, ActorID: &actorID, ProjectID: projectID,
			Action: action, Result: auditResult, ResourceType: "device_raw_message", ResourceID: &resourceID,
			Metadata: inboundAuditMetadata(request, result), OccurredAt: now,
		})
	})
	if err != nil {
		return InboundRecordResult{}, err
	}
	return result, nil
}

func resolveInboundDevice(ctx context.Context, devices repository.DeviceRepository, request InboundRecordRequest) (domain.Device, bool, string, error) {
	if request.Decoded.Err != nil {
		return domain.Device{}, false, "frame_" + string(ErrorCode(request.Decoded.Err)), nil
	}
	frame := request.Decoded.Frame
	if request.FirstFrame && frame.Command != "Q0" {
		return domain.Device{}, false, "handshake_required", nil
	}
	if !request.FirstFrame && frame.Command == "Q0" {
		return domain.Device{}, false, "unexpected_handshake", nil
	}

	var device domain.Device
	var err error
	if request.FirstFrame {
		device, err = devices.GetByProviderIdentity(ctx, domain.ProviderCodeOmni, frame.IMEI)
		if err == nil {
			device, err = devices.GetForUpdate(ctx, device.ID)
		}
	} else {
		if request.ExpectedDeviceID == "" {
			return domain.Device{}, false, "handshake_required", nil
		}
		device, err = devices.GetForUpdate(ctx, request.ExpectedDeviceID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, false, "device_not_registered", nil
	}
	if err != nil {
		return domain.Device{}, false, "", err
	}
	if device.ProviderCode != domain.ProviderCodeOmni || device.ProviderProfile != request.Profile ||
		device.ProviderDeviceID != frame.IMEI || device.AccessType != domain.AccessTypeDirectDevice ||
		device.TransportProtocol != domain.TransportProtocolTCP || device.Adapter != domain.AdapterOmniDirectTCP {
		code := "device_binding_mismatch"
		if !request.FirstFrame && device.ProviderDeviceID != frame.IMEI {
			code = "session_identity_mismatch"
		}
		return domain.Device{}, false, code, nil
	}
	if device.LifecycleStatus != domain.LifecycleStatusActive {
		return domain.Device{}, false, "device_not_active", nil
	}
	return device, true, "", nil
}

func inboundDeduplicationKey(request InboundRecordRequest) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(request.Profile))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(request.ConnectionID))
	_, _ = hash.Write([]byte{0})
	if len(request.Decoded.Raw) > 0 {
		_, _ = hash.Write(request.Decoded.Raw)
	} else {
		_, _ = hash.Write([]byte(ErrorCode(request.Decoded.Err)))
	}
	return request.Profile + ":" + hex.EncodeToString(hash.Sum(nil))
}

func inboundSafeSummary(request InboundRecordRequest, rejectCode string) map[string]any {
	summary := map[string]any{
		"provider_profile": request.Profile,
		"frame_bytes":      len(request.Decoded.Raw),
		"parse_status":     inboundParseStatus(request.Decoded.Err),
		"evidence_status":  domain.EvidenceUnverified,
	}
	if request.Decoded.Err != nil {
		summary["error_code"] = string(ErrorCode(request.Decoded.Err))
	} else {
		summary["command"] = request.Decoded.Frame.Command
		summary["field_count"] = len(request.Decoded.Frame.Fields)
	}
	if rejectCode != "" {
		summary["reject_code"] = rejectCode
	}
	return summary
}

func inboundAuditMetadata(request InboundRecordRequest, result InboundRecordResult) map[string]any {
	metadata := inboundSafeSummary(request, result.RejectCode)
	metadata["provider_code"] = domain.ProviderCodeOmni
	metadata["adapter"] = domain.AdapterOmniDirectTCP
	metadata["connection_id"] = request.ConnectionID
	metadata["duplicate"] = result.Duplicate
	metadata["raw_message_id"] = result.RawMessageID
	return metadata
}

func inboundParseStatus(err error) string {
	if err != nil {
		return "invalid"
	}
	return "valid"
}

func (r *PersistentInboundRecorder) newIDs() (string, string, error) {
	r.randomMu.Lock()
	defer r.randomMu.Unlock()
	messageID, err := inboundUUID(r.random)
	if err != nil {
		return "", "", err
	}
	auditID, err := inboundUUID(r.random)
	return messageID, auditID, err
}

func inboundUUID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

var _ InboundRecorder = (*PersistentInboundRecorder)(nil)

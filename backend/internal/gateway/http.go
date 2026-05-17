package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/httpjson"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(mux *http.ServeMux) {
	h.RegisterSimulator(mux)
	mux.HandleFunc("/v1/open/device-commands", h.deviceCommands)
	mux.HandleFunc("/v1/open/device-commands/", h.deviceCommandByID)
}

func (h *Handler) RegisterSimulator(mux *http.ServeMux) {
	mux.HandleFunc("/v1/simulator", h.gatewayConfig)
	mux.HandleFunc("/v1/simulator/gateway", h.gatewayConfig)
}

func (h *Handler) gatewayConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, "ok", snapshotResponse(h.service.Snapshot()))
	case http.MethodPatch, http.MethodPut, http.MethodPost:
		var request modeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
			return
		}
		mode, err := ParseMode(request.Mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_mode", "invalid simulator mode")
			return
		}
		if err := h.service.SetMode(ModeConfig{
			Mode:          mode,
			Delay:         durationFromMillis(request.DelayMillis),
			TimeoutOffset: durationFromMillis(request.TimeoutOffsetMillis),
			Heartbeat:     durationFromMillis(request.HeartbeatMillis),
		}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_mode", "invalid simulator mode")
			return
		}
		writeJSON(w, http.StatusOK, "ok", snapshotResponse(h.service.Snapshot()))
	default:
		w.Header().Set("Allow", "GET, PATCH, POST, PUT")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (h *Handler) deviceCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var request commandRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if strings.TrimSpace(request.CommandType) == "" {
		writeError(w, http.StatusBadRequest, "command_type_required", "command_type is required")
		return
	}

	record, err := h.service.CreateCommand(r.Context(), request.CommandType, request.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command_create_failed", "failed to create command")
		return
	}
	writeJSON(w, http.StatusAccepted, "accepted", record)
}

func (h *Handler) deviceCommandByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/v1/open/device-commands/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	record, err := h.service.GetCommand(id)
	if errors.Is(err, ErrCommandNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command_read_failed", "failed to read command")
		return
	}
	writeJSON(w, http.StatusOK, "ok", record)
}

type modeRequest struct {
	Mode                string `json:"mode"`
	DelayMillis         int64  `json:"delay_ms"`
	TimeoutOffsetMillis int64  `json:"timeout_offset_ms"`
	HeartbeatMillis     int64  `json:"heartbeat_ms"`
}

type commandRequest struct {
	DeviceID    string         `json:"device_id"`
	CommandType string         `json:"command_type"`
	Payload     map[string]any `json:"payload"`
}

type gatewayResponse struct {
	Mode                Mode      `json:"mode"`
	DelayMillis         int64     `json:"delay_ms"`
	TimeoutOffsetMillis int64     `json:"timeout_offset_ms"`
	HeartbeatMillis     int64     `json:"heartbeat_ms"`
	HeartbeatActive     bool      `json:"heartbeat_active"`
	Online              bool      `json:"online"`
	LastHeartbeat       time.Time `json:"last_heartbeat,omitempty"`
	UpdatedAt           time.Time `json:"updated_at,omitempty"`
}

func snapshotResponse(snapshot Snapshot) gatewayResponse {
	return gatewayResponse{
		Mode:                snapshot.Mode,
		DelayMillis:         snapshot.Delay.Milliseconds(),
		TimeoutOffsetMillis: snapshot.TimeoutOffset.Milliseconds(),
		HeartbeatMillis:     snapshot.Heartbeat.Milliseconds(),
		HeartbeatActive:     snapshot.Online,
		Online:              snapshot.Online,
		LastHeartbeat:       snapshot.LastHeartbeat,
		UpdatedAt:           time.Now(),
	}
}

func durationFromMillis(value int64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Millisecond
}

func writeJSON(w http.ResponseWriter, status int, message string, payload any) {
	httpjson.Write(w, status, message, payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpjson.Error(w, status, code, message)
}

package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/httpjson"
)

type Router struct {
	service          DeviceService
	onCommandCreated func(*http.Request, devicecore.Command)
}

type DeviceService interface {
	CreateProject(devicecore.CreateProjectRequest) (devicecore.Project, error)
	ListProjects() []devicecore.Project
	GetProject(string) (devicecore.Project, error)
	ProjectByAPIKey(string) (devicecore.Project, error)
	UpdateProject(string, devicecore.UpdateProjectRequest) (devicecore.Project, error)
	CreateDevice(devicecore.CreateDeviceRequest) (devicecore.Device, error)
	ListDevices(string) []devicecore.Device
	GetDevice(string, string) (devicecore.Device, error)
	SetDeviceOnline(string, string, bool) error
	CreateCommand(devicecore.CreateCommandRequest) (devicecore.Command, error)
	ListCommands(string) []devicecore.Command
	GetCommand(string, string) (devicecore.Command, error)
	CancelCommand(string, string) (devicecore.Command, error)
}

type RouterHooks struct {
	OnCommandCreated func(*http.Request, devicecore.Command)
}

func NewRouter(service DeviceService) http.Handler {
	return NewRouterWithHooks(service, RouterHooks{})
}

func NewRouterWithHooks(service DeviceService, hooks RouterHooks) http.Handler {
	r := &Router{service: service, onCommandCreated: hooks.OnCommandCreated}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects", r.handleProjects)
	mux.HandleFunc("/v1/projects/", r.handleProjectByID)
	mux.HandleFunc("/v1/devices", r.handleDevices)
	mux.HandleFunc("/v1/devices/", r.handleDeviceByID)
	mux.HandleFunc("/v1/device-commands", r.handleAdminCommands)
	mux.HandleFunc("/v1/device-commands/", r.handleAdminCommandByID)
	return mux
}

func NewOpenRouter(service DeviceService) http.Handler {
	return NewOpenRouterWithHooks(service, RouterHooks{})
}

func NewOpenRouterWithHooks(service DeviceService, hooks RouterHooks) http.Handler {
	r := &Router{service: service, onCommandCreated: hooks.OnCommandCreated}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/open/projects/", r.handleOpenProjectByID)
	mux.HandleFunc("/v1/open/devices", r.handleOpenDevices)
	mux.HandleFunc("/v1/open/devices/", r.handleOpenDeviceByID)
	mux.HandleFunc("/v1/open/device-commands", r.handleOpenCommands)
	mux.HandleFunc("/v1/open/device-commands/", r.handleOpenCommandByID)
	return mux
}

func (r *Router) handleAdminCommands(w http.ResponseWriter, req *http.Request) {
	projectID := strings.TrimSpace(req.Header.Get("X-Project-ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(req.URL.Query().Get("project_id"))
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, "ok", r.service.ListCommands(projectID))
	case http.MethodPost:
		var body devicecore.CreateCommandRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		if body.ProjectID == "" {
			body.ProjectID = projectID
		}
		command, err := r.service.CreateCommand(body)
		if err == nil && r.onCommandCreated != nil {
			r.onCommandCreated(req, command)
		}
		writeResult(w, command, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleAdminCommandByID(w http.ResponseWriter, req *http.Request) {
	projectID := strings.TrimSpace(req.Header.Get("X-Project-ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(req.URL.Query().Get("project_id"))
	}
	path := strings.TrimPrefix(req.URL.Path, "/v1/device-commands/")
	commandID, action, _ := strings.Cut(path, "/")
	if commandID == "" {
		notFound(w)
		return
	}
	if action == "cancel" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		command, err := r.service.CancelCommand(projectID, commandID)
		writeResult(w, command, err, http.StatusOK)
		return
	}
	if action != "" || req.Method != http.MethodGet {
		notFound(w)
		return
	}
	command, err := r.service.GetCommand(projectID, commandID)
	if err != nil {
		writeResult(w, nil, err, http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"command":  command,
		"attempts": command.Attempts,
		"events":   command.Events,
	})
}

func (r *Router) handleProjects(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, "ok", r.service.ListProjects())
	case http.MethodPost:
		var body devicecore.CreateProjectRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		project, err := r.service.CreateProject(body)
		writeResult(w, project, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleProjectByID(w http.ResponseWriter, req *http.Request) {
	projectID := strings.TrimPrefix(req.URL.Path, "/v1/projects/")
	if projectID == "" || strings.Contains(projectID, "/") {
		notFound(w)
		return
	}
	switch req.Method {
	case http.MethodGet:
		project, err := r.service.GetProject(projectID)
		writeResult(w, project, err, http.StatusOK)
	case http.MethodPatch:
		var body devicecore.UpdateProjectRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		project, err := r.service.UpdateProject(projectID, body)
		writeResult(w, project, err, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleDevices(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		projectID := req.URL.Query().Get("project_id")
		writeJSON(w, http.StatusOK, "ok", r.service.ListDevices(projectID))
	case http.MethodPost:
		var body devicecore.CreateDeviceRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		device, err := r.service.CreateDevice(body)
		writeResult(w, device, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleDeviceByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/v1/devices/")
	deviceID, action, _ := strings.Cut(path, "/")
	if deviceID == "" {
		notFound(w)
		return
	}
	projectID := req.URL.Query().Get("project_id")
	switch req.Method {
	case http.MethodGet:
		if action != "" {
			notFound(w)
			return
		}
		device, err := r.service.GetDevice(projectID, deviceID)
		writeResult(w, device, err, http.StatusOK)
	case http.MethodPost:
		if action != "online" {
			notFound(w)
			return
		}
		var body struct {
			Online bool `json:"online"`
		}
		if !decodeJSON(w, req, &body) {
			return
		}
		err := r.service.SetDeviceOnline(projectID, deviceID, body.Online)
		writeResult(w, map[string]any{"ok": err == nil}, err, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleOpenProjectByID(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	projectID := strings.TrimPrefix(req.URL.Path, "/v1/open/projects/")
	if req.Method != http.MethodGet || projectID != project.ID {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, "ok", project)
}

func (r *Router) handleOpenDevices(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, "ok", r.service.ListDevices(project.ID))
}

func (r *Router) handleOpenDeviceByID(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	deviceID := strings.TrimPrefix(req.URL.Path, "/v1/open/devices/")
	device, err := r.service.GetDevice(project.ID, deviceID)
	writeResult(w, device, err, http.StatusOK)
}

func (r *Router) handleOpenCommands(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, "ok", r.service.ListCommands(project.ID))
	case http.MethodPost:
		var body devicecore.CreateCommandRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		body.ProjectID = project.ID
		command, err := r.service.CreateCommand(body)
		if err == nil && r.onCommandCreated != nil {
			r.onCommandCreated(req, command)
		}
		writeResult(w, command, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleOpenCommandByID(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	path := strings.TrimPrefix(req.URL.Path, "/v1/open/device-commands/")
	commandID, action, _ := strings.Cut(path, "/")
	if commandID == "" {
		notFound(w)
		return
	}
	if action == "cancel" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		command, err := r.service.CancelCommand(project.ID, commandID)
		writeResult(w, command, err, http.StatusOK)
		return
	}
	if action != "" || req.Method != http.MethodGet {
		notFound(w)
		return
	}
	command, err := r.service.GetCommand(project.ID, commandID)
	writeResult(w, command, err, http.StatusOK)
}

func (r *Router) authenticateOpen(w http.ResponseWriter, req *http.Request) (devicecore.Project, bool) {
	apiKey := strings.TrimSpace(req.Header.Get("X-API-Key"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key", "missing API key")
		return devicecore.Project{}, false
	}
	project, err := r.service.ProjectByAPIKey(apiKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "invalid API key")
		return devicecore.Project{}, false
	}
	return project, true
}

func decodeJSON(w http.ResponseWriter, req *http.Request, out any) bool {
	defer req.Body.Close()
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			writeError(w, http.StatusBadRequest, "unknown_field", err.Error())
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, value any, err error, successStatus int) {
	if err == nil {
		message := "ok"
		if successStatus == http.StatusCreated {
			message = "created"
		}
		writeJSON(w, successStatus, message, value)
		return
	}
	switch {
	case errors.Is(err, devicecore.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, devicecore.ErrDuplicateDevice):
		writeError(w, http.StatusConflict, "duplicate_device", "provider device already exists")
	case errors.Is(err, devicecore.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_key_conflict", "idempotency key conflict")
	case errors.Is(err, devicecore.ErrUnsafeDeliveryOverride):
		writeError(w, http.StatusBadRequest, "unsafe_delivery_policy_override", err.Error())
	case errors.Is(err, devicecore.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
	case errors.Is(err, devicecore.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_command_transition", "invalid command transition")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, message string, value any) {
	httpjson.Write(w, status, message, value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpjson.Error(w, status, code, message)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not_found", "resource not found")
}

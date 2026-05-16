package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/devicecore"
)

type Router struct {
	service *devicecore.Service
}

func NewRouter(service *devicecore.Service) http.Handler {
	r := &Router{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects", r.handleProjects)
	mux.HandleFunc("/v1/projects/", r.handleProjectByID)
	mux.HandleFunc("/v1/devices", r.handleDevices)
	mux.HandleFunc("/v1/devices/", r.handleDeviceByID)
	return mux
}

func NewOpenRouter(service *devicecore.Service) http.Handler {
	r := &Router{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/open/projects/", r.handleOpenProjectByID)
	mux.HandleFunc("/v1/open/devices", r.handleOpenDevices)
	mux.HandleFunc("/v1/open/devices/", r.handleOpenDeviceByID)
	mux.HandleFunc("/v1/open/device-commands", r.handleOpenCommands)
	mux.HandleFunc("/v1/open/device-commands/", r.handleOpenCommandByID)
	return mux
}

func (r *Router) handleProjects(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, r.service.ListProjects())
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
		writeJSON(w, http.StatusOK, r.service.ListDevices(projectID))
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
	writeJSON(w, http.StatusOK, project)
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
	writeJSON(w, http.StatusOK, r.service.ListDevices(project.ID))
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
		writeJSON(w, http.StatusOK, r.service.ListCommands(project.ID))
	case http.MethodPost:
		var body devicecore.CreateCommandRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		body.ProjectID = project.ID
		command, err := r.service.CreateCommand(body)
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
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return devicecore.Project{}, false
	}
	project, err := r.service.ProjectByAPIKey(apiKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_api_key")
		return devicecore.Project{}, false
	}
	return project, true
}

func decodeJSON(w http.ResponseWriter, req *http.Request, out any) bool {
	defer req.Body.Close()
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, value any, err error, successStatus int) {
	if err == nil {
		writeJSON(w, successStatus, value)
		return
	}
	switch {
	case errors.Is(err, devicecore.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, devicecore.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_key_conflict")
	case errors.Is(err, devicecore.ErrUnsafeDeliveryOverride), errors.Is(err, devicecore.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, devicecore.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_command_transition")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not_found")
}

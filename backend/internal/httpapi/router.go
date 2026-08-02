package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/access"
	v1 "github.com/qiyue2015/device-platform/internal/api/v1"
	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/projectservice"
)

type Router struct {
	service          DeviceService
	projects         ProjectService
	devices          DeviceResourceService
	commands         CommandResourceService
	onCommandCreated func(*http.Request, devicecore.Command)
	projectMetadata  func(*http.Request) projectservice.RequestMetadata
	deviceMetadata   func(*http.Request) deviceservice.RequestMetadata
	commandMetadata  func(*http.Request) commandservice.RequestMetadata
	humanScope       func(*http.Request) access.Scope
}

type ProjectService interface {
	Create(context.Context, projectservice.Scope, projectservice.CreateRequest, projectservice.RequestMetadata) (projectservice.CredentialResult, error)
	List(context.Context, projectservice.Scope, projectservice.ListRequest) (projectservice.ListResult, error)
	Get(context.Context, projectservice.Scope, string) (projectservice.Project, error)
	Update(context.Context, projectservice.Scope, string, projectservice.UpdateRequest, projectservice.RequestMetadata) (projectservice.UpdateResult, error)
	RotateAPIKey(context.Context, projectservice.Scope, string, projectservice.RequestMetadata) (projectservice.CredentialResult, error)
	RotateWebhookSecret(context.Context, projectservice.Scope, string, projectservice.RequestMetadata) (projectservice.CredentialResult, error)
	Transfer(context.Context, projectservice.Scope, string, projectservice.TransferRequest, projectservice.RequestMetadata) (projectservice.Project, error)
	AuthenticateAPIKey(context.Context, string, string) (projectservice.Project, error)
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

type DeviceResourceService interface {
	ListDeviceTypes(context.Context) ([]deviceservice.DeviceType, error)
	GetDeviceType(context.Context, string) (deviceservice.DeviceType, error)
	ListProviders() []deviceservice.Provider
	Create(context.Context, deviceservice.Scope, deviceservice.CreateRequest, deviceservice.RequestMetadata) (deviceservice.Device, error)
	List(context.Context, deviceservice.Scope, deviceservice.ListRequest) (deviceservice.ListResult, error)
	Get(context.Context, deviceservice.Scope, string) (deviceservice.Device, error)
	Update(context.Context, deviceservice.Scope, string, deviceservice.UpdateRequest, deviceservice.RequestMetadata) (deviceservice.Device, error)
}

type CommandResourceService interface {
	Create(context.Context, commandservice.Scope, commandservice.CreateRequest, commandservice.RequestMetadata) (commandservice.CreateResult, error)
	List(context.Context, commandservice.Scope, commandservice.ListRequest) (commandservice.ListResult, error)
	Get(context.Context, commandservice.Scope, string) (commandservice.Detail, error)
	Cancel(context.Context, commandservice.Scope, string, commandservice.RequestMetadata) (domain.Command, error)
}

type RouterHooks struct {
	OnCommandCreated func(*http.Request, devicecore.Command)
	ProjectMetadata  func(*http.Request) projectservice.RequestMetadata
	DeviceMetadata   func(*http.Request) deviceservice.RequestMetadata
	CommandMetadata  func(*http.Request) commandservice.RequestMetadata
	HumanScope       func(*http.Request) access.Scope
}

func NewRouter(service DeviceService) http.Handler {
	return NewRouterWithHooks(service, RouterHooks{})
}

func NewRouterWithHooks(service DeviceService, hooks RouterHooks) http.Handler {
	return NewRouterWithProjectService(service, legacyProjectService{service: service}, hooks)
}

func NewRouterWithProjectService(service DeviceService, projects ProjectService, hooks RouterHooks) http.Handler {
	return NewRouterWithResourceServices(service, projects, nil, hooks)
}

func NewRouterWithResourceServices(service DeviceService, projects ProjectService, devices DeviceResourceService, hooks RouterHooks) http.Handler {
	return NewRouterWithDomainServices(service, projects, devices, nil, hooks)
}

func NewRouterWithDomainServices(service DeviceService, projects ProjectService, devices DeviceResourceService, commands CommandResourceService, hooks RouterHooks) http.Handler {
	r := newRouter(service, projects, devices, commands, hooks)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects", r.handleProjects)
	mux.HandleFunc("/v1/projects/", r.handleProjectByID)
	if devices != nil {
		mux.HandleFunc("/v1/device-types", r.handleDeviceTypes)
		mux.HandleFunc("/v1/device-types/", r.handleDeviceTypeByCode)
	}
	mux.HandleFunc("/v1/devices", r.handleDevices)
	mux.HandleFunc("/v1/devices/", r.handleDeviceByID)
	mux.HandleFunc("/v1/device-commands", r.handleAdminCommands)
	mux.HandleFunc("/v1/device-commands/", r.handleAdminCommandByID)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, _ *http.Request) {
		notFound(w)
	})
	return mux
}

func NewOpenRouter(service DeviceService) http.Handler {
	return NewOpenRouterWithHooks(service, RouterHooks{})
}

func NewOpenRouterWithHooks(service DeviceService, hooks RouterHooks) http.Handler {
	return NewOpenRouterWithProjectService(service, legacyProjectService{service: service}, hooks)
}

func NewOpenRouterWithProjectService(service DeviceService, projects ProjectService, hooks RouterHooks) http.Handler {
	return NewOpenRouterWithResourceServices(service, projects, nil, hooks)
}

func NewOpenRouterWithResourceServices(service DeviceService, projects ProjectService, devices DeviceResourceService, hooks RouterHooks) http.Handler {
	return NewOpenRouterWithDomainServices(service, projects, devices, nil, hooks)
}

func NewOpenRouterWithDomainServices(service DeviceService, projects ProjectService, devices DeviceResourceService, commands CommandResourceService, hooks RouterHooks) http.Handler {
	r := newRouter(service, projects, devices, commands, hooks)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/open/projects/", r.handleOpenProjectByID)
	mux.HandleFunc("/v1/open/devices", r.handleOpenDevices)
	mux.HandleFunc("/v1/open/devices/", r.handleOpenDeviceByID)
	mux.HandleFunc("/v1/open/device-commands", r.handleOpenCommands)
	mux.HandleFunc("/v1/open/device-commands/", r.handleOpenCommandByID)
	mux.HandleFunc("/v1/open/", func(w http.ResponseWriter, _ *http.Request) {
		notFound(w)
	})
	return mux
}

func newRouter(service DeviceService, projects ProjectService, devices DeviceResourceService, commands CommandResourceService, hooks RouterHooks) *Router {
	projectMetadata := hooks.ProjectMetadata
	if projectMetadata == nil {
		projectMetadata = defaultProjectMetadata
	}
	deviceMetadata := hooks.DeviceMetadata
	if deviceMetadata == nil {
		deviceMetadata = defaultDeviceMetadata
	}
	commandMetadata := hooks.CommandMetadata
	if commandMetadata == nil {
		commandMetadata = defaultCommandMetadata
	}
	humanScope := hooks.HumanScope
	if humanScope == nil {
		humanScope = defaultHumanScope
	}
	return &Router{
		service: service, projects: projects, devices: devices, commands: commands, onCommandCreated: hooks.OnCommandCreated,
		projectMetadata: projectMetadata, deviceMetadata: deviceMetadata, commandMetadata: commandMetadata, humanScope: humanScope,
	}
}

func (r *Router) handleAdminCommands(w http.ResponseWriter, req *http.Request) {
	if r.commands != nil {
		r.handleAdminCommandResources(w, req)
		return
	}
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
	if r.commands != nil {
		r.handleAdminCommandResourceByID(w, req)
		return
	}
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

func (r *Router) handleAdminCommandResources(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		listRequest, ok := parseCommandListRequest(w, req, true)
		if !ok {
			return
		}
		result, err := r.commands.List(req.Context(), r.humanScope(req), listRequest)
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeCommandList(w, result)
	case http.MethodPost:
		if !rejectQueryParameters(w, req) {
			return
		}
		var body v1.CreateCommandRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		result, err := r.commands.Create(req.Context(), r.humanScope(req), commandservice.CreateRequest{
			ProjectID: body.ProjectID, DeviceID: body.DeviceID, CommandType: body.CommandType,
			Payload: map[string]any(body.Payload), IdempotencyKey: body.IdempotencyKey,
		}, r.commandMetadata(req))
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeCommandCreated(w, result)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleAdminCommandResourceByID(w http.ResponseWriter, req *http.Request) {
	commandID, action, ok := commandResourcePath(req.URL.Path, "/v1/device-commands/")
	if !ok {
		notFound(w)
		return
	}
	if action == "cancel" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !rejectQueryParameters(w, req) || !decodeOptionalEmptyJSON(w, req) {
			return
		}
		command, err := r.commands.Cancel(req.Context(), r.humanScope(req), commandID, r.commandMetadata(req))
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", commandResponse(command))
		return
	}
	if action != "" {
		notFound(w)
		return
	}
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !rejectQueryParameters(w, req) {
		return
	}
	detail, err := r.commands.Get(req.Context(), r.humanScope(req), commandID)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, "ok", commandDetailResponse(detail))
}

func (r *Router) handleProjects(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		listRequest, ok := parseProjectListRequest(w, req)
		if !ok {
			return
		}
		result, err := r.projects.List(req.Context(), r.humanScope(req), listRequest)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		items := make([]v1.ProjectResponse, 0, len(result.Items))
		for _, project := range result.Items {
			items = append(items, projectResponse(project))
		}
		httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": items}, map[string]any{
			"page": result.Page, "page_size": result.PageSize, "total": result.Total,
		})
	case http.MethodPost:
		if !rejectQueryParameters(w, req) {
			return
		}
		var body v1.CreateProjectRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		result, err := r.projects.Create(req.Context(), r.humanScope(req), projectservice.CreateRequest{
			Name: body.Name, ManagerUserID: body.ManagerUserID, WebhookURL: body.WebhookURL, IPWhitelist: body.IPWhitelist,
		}, r.projectMetadata(req))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, "created", credentialResponse(result))
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleProjectByID(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/v1/projects/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		notFound(w)
		return
	}
	projectID := parts[0]
	if !rejectQueryParameters(w, req) {
		return
	}
	if len(parts) == 2 && parts[1] == "transfer" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body v1.TransferProjectRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		project, err := r.projects.Transfer(req.Context(), r.humanScope(req), projectID, projectservice.TransferRequest{
			ManagerUserID: body.ManagerUserID,
		}, r.projectMetadata(req))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", projectResponse(project))
		return
	}
	if len(parts) == 3 && parts[2] == "rotate" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !decodeOptionalEmptyJSON(w, req) {
			return
		}
		var result projectservice.CredentialResult
		var err error
		switch parts[1] {
		case "api-key":
			result, err = r.projects.RotateAPIKey(req.Context(), r.humanScope(req), projectID, r.projectMetadata(req))
		case "webhook-secret":
			result, err = r.projects.RotateWebhookSecret(req.Context(), r.humanScope(req), projectID, r.projectMetadata(req))
		default:
			notFound(w)
			return
		}
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", credentialResponse(result))
		return
	}
	if len(parts) != 1 {
		notFound(w)
		return
	}
	switch req.Method {
	case http.MethodGet:
		project, err := r.projects.Get(req.Context(), r.humanScope(req), projectID)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", projectResponse(project))
	case http.MethodPatch:
		var body projectUpdateBody
		if !decodeJSON(w, req, &body) {
			return
		}
		result, err := r.projects.Update(req.Context(), r.humanScope(req), projectID, body.request(), r.projectMetadata(req))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		response := v1.ProjectCredentialResponse{ProjectResponse: projectResponse(result.Project), WebhookSecret: result.WebhookSecret}
		writeJSON(w, http.StatusOK, "ok", response)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleDeviceTypes(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, pageSize, ok := parsePageOnlyListRequest(w, req)
	if !ok {
		return
	}
	deviceTypes, err := r.devices.ListDeviceTypes(req.Context())
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	start := (page - 1) * pageSize
	if start > len(deviceTypes) {
		start = len(deviceTypes)
	}
	end := min(start+pageSize, len(deviceTypes))
	items := make([]v1.DeviceTypeResponse, 0, end-start)
	for _, deviceType := range deviceTypes[start:end] {
		items = append(items, deviceTypeResponse(deviceType))
	}
	httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": items}, map[string]any{
		"page": page, "page_size": pageSize, "total": len(deviceTypes),
	})
}

func (r *Router) handleDeviceTypeByCode(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	code := strings.TrimPrefix(req.URL.Path, "/v1/device-types/")
	if code == "" || strings.Contains(code, "/") {
		notFound(w)
		return
	}
	if !rejectQueryParameters(w, req) {
		return
	}
	deviceType, err := r.devices.GetDeviceType(req.Context(), code)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, "ok", deviceTypeResponse(deviceType))
}

func (r *Router) handleDevices(w http.ResponseWriter, req *http.Request) {
	if r.devices == nil {
		r.handleLegacyDevices(w, req)
		return
	}
	switch req.Method {
	case http.MethodGet:
		listRequest, ok := parseDeviceListRequest(w, req, true)
		if !ok {
			return
		}
		result, err := r.devices.List(req.Context(), r.humanScope(req), listRequest)
		if err != nil {
			writeDeviceError(w, err)
			return
		}
		writeDeviceList(w, result)
	case http.MethodPost:
		if !rejectQueryParameters(w, req) {
			return
		}
		var body deviceCreateBody
		if !decodeJSON(w, req, &body) {
			return
		}
		if body.ProviderDeviceID.Set && body.ProviderDeviceID.Value == nil {
			writeDeviceError(w, deviceservice.ErrInvalidRequest)
			return
		}
		device, err := r.devices.Create(req.Context(), r.humanScope(req), deviceservice.CreateRequest{
			ProjectID: body.ProjectID, Name: body.Name, DeviceTypeCode: body.DeviceTypeCode,
			ProviderCode: body.ProviderCode, ProviderProfile: body.ProviderProfile,
			ProviderDeviceID: body.ProviderDeviceID.Value,
		}, r.deviceMetadata(req))
		if err != nil {
			writeDeviceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, "created", deviceResponse(device))
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleDeviceByID(w http.ResponseWriter, req *http.Request) {
	if r.devices == nil {
		r.handleLegacyDeviceByID(w, req)
		return
	}
	path := strings.TrimPrefix(req.URL.Path, "/v1/devices/")
	if path == "" || strings.Contains(path, "/") {
		notFound(w)
		return
	}
	if !rejectQueryParameters(w, req) {
		return
	}
	switch req.Method {
	case http.MethodGet:
		device, err := r.devices.Get(req.Context(), r.humanScope(req), path)
		if err != nil {
			writeDeviceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", deviceResponse(device))
	case http.MethodPatch:
		var body deviceUpdateBody
		if !decodeJSON(w, req, &body) {
			return
		}
		device, err := r.devices.Update(req.Context(), r.humanScope(req), path, deviceservice.UpdateRequest{
			Name: body.name(), LifecycleStatus: body.lifecycleStatus(),
		}, r.deviceMetadata(req))
		if err != nil {
			writeDeviceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", deviceResponse(device))
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleLegacyDevices(w http.ResponseWriter, req *http.Request) {
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

func (r *Router) handleLegacyDeviceByID(w http.ResponseWriter, req *http.Request) {
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
	if !rejectQueryParameters(w, req) {
		return
	}
	projectID := strings.TrimPrefix(req.URL.Path, "/v1/open/projects/")
	if req.Method != http.MethodGet || projectID != project.ID {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, "ok", openProjectResponse(project))
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
	if r.devices == nil {
		writeJSON(w, http.StatusOK, "ok", r.service.ListDevices(project.ID))
		return
	}
	listRequest, ok := parseDeviceListRequest(w, req, false)
	if !ok {
		return
	}
	result, err := r.devices.List(req.Context(), deviceservice.ProjectScope(project.ID), listRequest)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeDeviceList(w, result)
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
	if deviceID == "" || strings.Contains(deviceID, "/") {
		notFound(w)
		return
	}
	if !rejectQueryParameters(w, req) {
		return
	}
	if r.devices == nil {
		device, err := r.service.GetDevice(project.ID, deviceID)
		writeResult(w, device, err, http.StatusOK)
		return
	}
	device, err := r.devices.Get(req.Context(), deviceservice.ProjectScope(project.ID), deviceID)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, "ok", deviceResponse(device))
}

func (r *Router) handleOpenCommands(w http.ResponseWriter, req *http.Request) {
	project, ok := r.authenticateOpen(w, req)
	if !ok {
		return
	}
	if r.commands != nil {
		r.handleOpenCommandResources(w, req, project)
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
	if r.commands != nil {
		r.handleOpenCommandResourceByID(w, req, project)
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

func (r *Router) handleOpenCommandResources(w http.ResponseWriter, req *http.Request, project projectservice.Project) {
	scope := commandservice.ProjectScope(project.ID)
	switch req.Method {
	case http.MethodGet:
		listRequest, ok := parseCommandListRequest(w, req, false)
		if !ok {
			return
		}
		result, err := r.commands.List(req.Context(), scope, listRequest)
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeCommandList(w, result)
	case http.MethodPost:
		if !rejectQueryParameters(w, req) {
			return
		}
		var body v1.CreateCommandRequest
		if !decodeJSON(w, req, &body) {
			return
		}
		result, err := r.commands.Create(req.Context(), scope, commandservice.CreateRequest{
			ProjectID: body.ProjectID, DeviceID: body.DeviceID, CommandType: body.CommandType,
			Payload: map[string]any(body.Payload), IdempotencyKey: body.IdempotencyKey,
		}, projectCommandMetadata(req, project.ID))
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeCommandCreated(w, result)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) handleOpenCommandResourceByID(w http.ResponseWriter, req *http.Request, project projectservice.Project) {
	commandID, action, ok := commandResourcePath(req.URL.Path, "/v1/open/device-commands/")
	if !ok {
		notFound(w)
		return
	}
	scope := commandservice.ProjectScope(project.ID)
	if action == "cancel" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !rejectQueryParameters(w, req) || !decodeOptionalEmptyJSON(w, req) {
			return
		}
		command, err := r.commands.Cancel(req.Context(), scope, commandID, projectCommandMetadata(req, project.ID))
		if err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, "ok", commandResponse(command))
		return
	}
	if action != "" {
		notFound(w)
		return
	}
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !rejectQueryParameters(w, req) {
		return
	}
	detail, err := r.commands.Get(req.Context(), scope, commandID)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, "ok", commandDetailResponse(detail))
}

func commandResourcePath(path, prefix string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, prefix)
	commandID, action, found := strings.Cut(trimmed, "/")
	if commandID == "" || found && (action == "" || strings.Contains(action, "/")) {
		return "", "", false
	}
	return commandID, action, true
}

func (r *Router) authenticateOpen(w http.ResponseWriter, req *http.Request) (projectservice.Project, bool) {
	apiKey := req.Header.Get("X-API-Key")
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return projectservice.Project{}, false
	}
	project, err := r.projects.AuthenticateAPIKey(req.Context(), apiKey, req.RemoteAddr)
	if err != nil {
		if errors.Is(err, projectservice.ErrSourceIPNotAllowed) {
			writeError(w, http.StatusForbidden, "forbidden", "source IP is not allowed")
			return projectservice.Project{}, false
		}
		if errors.Is(err, projectservice.ErrAuthenticationFailed) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return projectservice.Project{}, false
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return projectservice.Project{}, false
	}
	return project, true
}

func decodeJSON(w http.ResponseWriter, req *http.Request, out any) bool {
	defer req.Body.Close()
	if err := httpjson.DecodeStrict(req.Body, out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return false
	}
	return true
}

func decodeOptionalEmptyJSON(w http.ResponseWriter, req *http.Request) bool {
	if req.Body == nil || req.ContentLength == 0 {
		return true
	}
	var body struct{}
	return decodeJSON(w, req, &body)
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

type optionalNullableString struct {
	Set   bool
	Value *string
}

func (value *optionalNullableString) UnmarshalJSON(data []byte) error {
	value.Set = true
	if string(data) == "null" {
		value.Value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type optionalString struct {
	Set   bool
	Value string
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.Set = true
	if string(data) == "null" {
		return errors.New("value must not be null")
	}
	return json.Unmarshal(data, &value.Value)
}

type optionalStrings struct {
	Set   bool
	Value []string
}

func (value *optionalStrings) UnmarshalJSON(data []byte) error {
	value.Set = true
	if string(data) == "null" {
		return errors.New("value must not be null")
	}
	return json.Unmarshal(data, &value.Value)
}

type optionalLifecycleStatus struct {
	Set   bool
	Value domain.LifecycleStatus
}

func (value *optionalLifecycleStatus) UnmarshalJSON(data []byte) error {
	value.Set = true
	if string(data) == "null" {
		return errors.New("value must not be null")
	}
	return json.Unmarshal(data, &value.Value)
}

type deviceCreateBody struct {
	ProjectID        string                 `json:"project_id"`
	Name             string                 `json:"name"`
	DeviceTypeCode   string                 `json:"device_type_code"`
	ProviderCode     string                 `json:"provider_code"`
	ProviderProfile  string                 `json:"provider_profile"`
	ProviderDeviceID optionalNullableString `json:"provider_device_id"`
}

type deviceUpdateBody struct {
	Name            optionalString          `json:"name"`
	LifecycleStatus optionalLifecycleStatus `json:"lifecycle_status"`
}

func (body deviceUpdateBody) name() *string {
	if !body.Name.Set {
		return nil
	}
	return &body.Name.Value
}

func (body deviceUpdateBody) lifecycleStatus() *domain.LifecycleStatus {
	if !body.LifecycleStatus.Set {
		return nil
	}
	return &body.LifecycleStatus.Value
}

type projectUpdateBody struct {
	Name        optionalString         `json:"name"`
	WebhookURL  optionalNullableString `json:"webhook_url"`
	IPWhitelist optionalStrings        `json:"ip_whitelist"`
}

func (body projectUpdateBody) request() projectservice.UpdateRequest {
	var name *string
	if body.Name.Set {
		name = &body.Name.Value
	}
	var whitelist *[]string
	if body.IPWhitelist.Set {
		whitelist = &body.IPWhitelist.Value
	}
	return projectservice.UpdateRequest{
		Name: name, WebhookURLSet: body.WebhookURL.Set, WebhookURL: body.WebhookURL.Value, IPWhitelist: whitelist,
	}
}

func parseProjectListRequest(w http.ResponseWriter, req *http.Request) (projectservice.ListRequest, bool) {
	query, ok := strictQuery(w, req)
	if !ok {
		return projectservice.ListRequest{}, false
	}
	for key, values := range query {
		if (key != "page" && key != "page_size" && key != "name" && key != "manager_user_id") || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid query parameters")
			return projectservice.ListRequest{}, false
		}
	}
	page, ok := parsePositiveQueryInteger(query, "page", 1)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "page must be a positive integer")
		return projectservice.ListRequest{}, false
	}
	pageSize, ok := parsePositiveQueryInteger(query, "page_size", 20)
	if !ok || pageSize > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "page_size must be between 1 and 100")
		return projectservice.ListRequest{}, false
	}
	var name *string
	if query.Has("name") {
		value := query.Get("name")
		name = &value
	}
	var managerUserID *string
	if query.Has("manager_user_id") {
		value := query.Get("manager_user_id")
		managerUserID = &value
	}
	return projectservice.ListRequest{Name: name, ManagerUserID: managerUserID, Page: page, PageSize: pageSize}, true
}

func parsePageOnlyListRequest(w http.ResponseWriter, req *http.Request) (int, int, bool) {
	query, ok := strictQuery(w, req)
	if !ok {
		return 0, 0, false
	}
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid query parameters")
			return 0, 0, false
		}
	}
	page, ok := parsePositiveQueryInteger(query, "page", 1)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "page must be a positive integer")
		return 0, 0, false
	}
	pageSize, ok := parsePositiveQueryInteger(query, "page_size", 20)
	if !ok || pageSize > 100 || page-1 > math.MaxInt/pageSize {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid pagination")
		return 0, 0, false
	}
	return page, pageSize, true
}

func parseDeviceListRequest(w http.ResponseWriter, req *http.Request, admin bool) (deviceservice.ListRequest, bool) {
	query, ok := strictQuery(w, req)
	if !ok {
		return deviceservice.ListRequest{}, false
	}
	allowed := map[string]bool{
		"page": true, "page_size": true, "device_type_code": true, "provider_code": true,
		"connection_status": true, "lifecycle_status": true,
	}
	if admin {
		allowed["project_id"] = true
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid query parameters")
			return deviceservice.ListRequest{}, false
		}
	}
	page, ok := parsePositiveQueryInteger(query, "page", 1)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "page must be a positive integer")
		return deviceservice.ListRequest{}, false
	}
	pageSize, ok := parsePositiveQueryInteger(query, "page_size", 20)
	if !ok || pageSize > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "page_size must be between 1 and 100")
		return deviceservice.ListRequest{}, false
	}
	result := deviceservice.ListRequest{Page: page, PageSize: pageSize}
	if admin && query.Has("project_id") {
		value := query.Get("project_id")
		result.ProjectID = &value
	}
	if query.Has("device_type_code") {
		value := query.Get("device_type_code")
		result.DeviceTypeCode = &value
	}
	if query.Has("provider_code") {
		value := query.Get("provider_code")
		result.ProviderCode = &value
	}
	if query.Has("connection_status") {
		value := domain.ConnectionStatus(query.Get("connection_status"))
		result.ConnectionStatus = &value
	}
	if query.Has("lifecycle_status") {
		value := domain.LifecycleStatus(query.Get("lifecycle_status"))
		result.LifecycleStatus = &value
	}
	return result, true
}

func parseCommandListRequest(w http.ResponseWriter, req *http.Request, admin bool) (commandservice.ListRequest, bool) {
	query, ok := strictQuery(w, req)
	if !ok {
		return commandservice.ListRequest{}, false
	}
	allowed := map[string]bool{"page": true, "page_size": true, "device_id": true, "command_type": true, "status": true}
	if admin {
		allowed["project_id"] = true
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid query parameters")
			return commandservice.ListRequest{}, false
		}
	}
	page, ok := parsePositiveQueryInteger(query, "page", 1)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "page must be a positive integer")
		return commandservice.ListRequest{}, false
	}
	pageSize, ok := parsePositiveQueryInteger(query, "page_size", 20)
	if !ok || pageSize > 100 || page-1 > math.MaxInt/pageSize {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid pagination")
		return commandservice.ListRequest{}, false
	}
	result := commandservice.ListRequest{Page: page, PageSize: pageSize}
	if admin && query.Has("project_id") {
		value := query.Get("project_id")
		result.ProjectID = &value
	}
	if query.Has("device_id") {
		value := query.Get("device_id")
		result.DeviceID = &value
	}
	if query.Has("command_type") {
		value := domain.ActionIdentifier(query.Get("command_type"))
		result.CommandType = &value
	}
	if query.Has("status") {
		value := domain.CommandStatus(query.Get("status"))
		result.Status = &value
	}
	return result, true
}

func strictQuery(w http.ResponseWriter, req *http.Request) (url.Values, bool) {
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid query parameters")
		return nil, false
	}
	return query, true
}

func rejectQueryParameters(w http.ResponseWriter, req *http.Request) bool {
	query, ok := strictQuery(w, req)
	if !ok {
		return false
	}
	if len(query) != 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameters are not allowed")
		return false
	}
	return true
}

func parsePositiveQueryInteger(query map[string][]string, key string, fallback int) (int, bool) {
	values, exists := query[key]
	if !exists {
		return fallback, true
	}
	raw := values[0]
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value > 0
}

func projectResponse(project projectservice.Project) v1.ProjectResponse {
	response := v1.ProjectResponse{
		ID: project.ID, Name: project.Name, ManagerUserID: project.ManagerUserID,
		Manager: v1.ProjectManagerResponse{
			ID: project.Manager.ID, Email: project.Manager.Email,
			DisplayName: project.Manager.DisplayName, Status: project.Manager.Status,
		},
		CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}
	if project.SensitiveConfigurationIncluded {
		response.WebhookURL = &project.WebhookURL
		response.WebhookConfigured = &project.WebhookConfigured
		response.IPWhitelist = &project.IPWhitelist
	}
	return response
}

func credentialResponse(result projectservice.CredentialResult) v1.ProjectCredentialResponse {
	return v1.ProjectCredentialResponse{
		ProjectResponse: projectResponse(result.Project), APIKey: result.APIKey, WebhookSecret: result.WebhookSecret,
	}
}

func openProjectResponse(project projectservice.Project) v1.OpenProjectResponse {
	return v1.OpenProjectResponse{
		ID: project.ID, Name: project.Name, CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}
}

func deviceTypeResponse(deviceType deviceservice.DeviceType) v1.DeviceTypeResponse {
	actions := make([]v1.CapabilityActionResponse, 0, len(deviceType.Actions))
	for _, action := range deviceType.Actions {
		actions = append(actions, v1.CapabilityActionResponse{
			Identifier: action.Identifier, PayloadSchema: action.PayloadSchema, RiskLevel: action.RiskLevel,
			DeliveryPolicy: action.DeliveryPolicy, DispatchDeadlineMS: action.DispatchDeadlineMS,
			ProviderRequestTimeoutMS:   action.ProviderRequestTimeoutMS,
			ResultObservationTimeoutMS: action.ResultObservationTimeoutMS, RetryAllowed: action.RetryAllowed,
			DeliveryPolicyOverrideAllowed: action.DeliveryPolicyOverrideAllowed,
		})
	}
	return v1.DeviceTypeResponse{Code: deviceType.Code, Revision: deviceType.Revision, Name: deviceType.Name, Actions: actions}
}

func deviceResponse(device deviceservice.Device) v1.DeviceResponse {
	var currentState *v1.DeviceStateResponse
	if device.CurrentState != nil {
		currentState = &v1.DeviceStateResponse{
			State: device.CurrentState.State, EvidenceStatus: device.CurrentState.EvidenceStatus,
			ReportedAt: device.CurrentState.ReportedAt, ObservedAt: device.CurrentState.ObservedAt,
		}
	}
	return v1.DeviceResponse{
		ID: device.ID, ProjectID: device.ProjectID, DeviceTypeCode: device.DeviceTypeCode, Name: device.Name,
		ProviderCode: device.ProviderCode, ProviderProfile: device.ProviderProfile,
		ProviderDeviceID: device.ProviderDeviceID, AccessType: device.AccessType,
		TransportProtocol: device.TransportProtocol, Adapter: device.Adapter, ConnectionStatus: device.ConnectionStatus,
		LifecycleStatus: device.LifecycleStatus, CurrentState: currentState, LastSeenAt: device.LastSeenAt,
		CreatedAt: device.CreatedAt, UpdatedAt: device.UpdatedAt,
	}
}

func writeDeviceList(w http.ResponseWriter, result deviceservice.ListResult) {
	items := make([]v1.DeviceResponse, 0, len(result.Items))
	for _, device := range result.Items {
		items = append(items, deviceResponse(device))
	}
	httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": items}, map[string]any{
		"page": result.Page, "page_size": result.PageSize, "total": result.Total,
	})
}

func writeDeviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, deviceservice.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.Is(err, deviceservice.ErrProjectNotFound), errors.Is(err, deviceservice.ErrDeviceTypeNotFound),
		errors.Is(err, deviceservice.ErrProviderNotFound), errors.Is(err, deviceservice.ErrProviderProfileNotFound),
		errors.Is(err, deviceservice.ErrDeviceNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, deviceservice.ErrProviderDeviceConflict):
		writeError(w, http.StatusConflict, "provider_device_conflict", "Provider Device identity already exists")
	case errors.Is(err, deviceservice.ErrDeviceImmutable):
		writeError(w, http.StatusConflict, "device_deleted", "Device is deleted")
	case errors.Is(err, deviceservice.ErrLifecycleTransition):
		writeError(w, http.StatusConflict, "invalid_state_transition", "Device lifecycle transition is not allowed")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func commandResponse(command domain.Command) v1.CommandResponse {
	return v1.CommandResponse{
		ID: command.ID, ProjectID: command.ProjectID, DeviceID: command.DeviceID,
		ProviderCode: command.ProviderCode, ProviderProfile: command.ProviderProfile, CommandType: command.CommandType,
		Payload: command.Payload, DeviceTypeRevision: command.DeviceTypeRevision, DeliveryPolicy: command.DeliveryPolicy,
		Status: command.Status, ReasonCode: command.ReasonCode, ReasonDetail: command.ReasonDetail,
		ConfirmationLevel: command.ConfirmationLevel, EvidenceStatus: command.EvidenceStatus,
		IdempotencyKey: command.IdempotencyKey, QueuedAt: command.QueuedAt.UTC(),
		DispatchDeadlineAt: command.DispatchDeadlineAt.UTC(), SentAt: utcTimePointer(command.SentAt),
		ResultDeadlineAt: utcTimePointer(command.ResultDeadlineAt), FinishedAt: utcTimePointer(command.FinishedAt),
		CreatedAt: command.CreatedAt.UTC(), UpdatedAt: command.UpdatedAt.UTC(),
	}
}

func commandDetailResponse(detail commandservice.Detail) v1.CommandDetailResponse {
	attempts := make([]v1.CommandAttemptResponse, 0, len(detail.Attempts))
	for _, attempt := range detail.Attempts {
		attempts = append(attempts, v1.CommandAttemptResponse{
			AttemptNo: attempt.AttemptNo, Phase: attempt.Phase, ProviderCode: attempt.ProviderCode, Adapter: attempt.Adapter,
			ProviderRequestKey: attempt.ProviderRequestKey, Outcome: attempt.Outcome,
			ConfirmationLevel: attempt.ConfirmationLevel, EvidenceStatus: attempt.EvidenceStatus,
			RequestSummary: attempt.RequestSummary, ResponseSummary: attempt.ResponseSummary,
			ReasonCode: attempt.ReasonCode, ErrorDetail: attempt.ErrorDetail, ClaimedAt: attempt.ClaimedAt.UTC(),
			DispatchingAt: utcTimePointer(attempt.DispatchingAt), CompletedAt: utcTimePointer(attempt.CompletedAt),
		})
	}
	events := make([]v1.EventResponse, 0, len(detail.Events))
	for _, event := range detail.Events {
		events = append(events, v1.EventResponse{
			EventID: event.ID, SchemaVersion: event.SchemaVersion, EventType: event.EventType,
			ProjectID: event.ProjectID, DeviceID: event.DeviceID, CommandID: event.CommandID,
			OccurredAt: event.OccurredAt.UTC(), Source: event.Source, Payload: event.Payload,
		})
	}
	results := make([]v1.CommandResultResponse, 0, len(detail.Results))
	for _, result := range detail.Results {
		results = append(results, v1.CommandResultResponse{
			ResultID: result.ID, AttemptID: result.AttemptID, Source: result.Source, Outcome: result.Outcome,
			ConfirmationLevel: result.ConfirmationLevel, EvidenceStatus: result.EvidenceStatus,
			ReportedAt: utcTimePointer(result.ReportedAt), ObservedAt: result.ObservedAt.UTC(), Late: result.Late,
		})
	}
	return v1.CommandDetailResponse{CommandResponse: commandResponse(detail.Command), Attempts: attempts, Results: results, Events: events}
}

func writeCommandList(w http.ResponseWriter, result commandservice.ListResult) {
	items := make([]v1.CommandResponse, 0, len(result.Items))
	for _, command := range result.Items {
		items = append(items, commandResponse(command))
	}
	httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": items}, map[string]any{
		"page": result.Page, "page_size": result.PageSize, "total": result.Total,
	})
}

func writeCommandCreated(w http.ResponseWriter, result commandservice.CreateResult) {
	if result.IdempotentReplay {
		httpjson.WriteWithMeta(w, http.StatusOK, "ok", commandResponse(result.Command), map[string]any{"idempotent_replay": true})
		return
	}
	writeJSON(w, http.StatusCreated, "created", commandResponse(result.Command))
}

func writeCommandError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, commandservice.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.Is(err, commandservice.ErrProjectNotFound), errors.Is(err, commandservice.ErrDeviceNotFound),
		errors.Is(err, commandservice.ErrCommandNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, commandservice.ErrDeviceDisabled):
		writeError(w, http.StatusConflict, "device_disabled", "Device cannot accept Commands")
	case errors.Is(err, commandservice.ErrDeviceNotOnline):
		writeError(w, http.StatusConflict, "device_not_online", "Device is not online")
	case errors.Is(err, commandservice.ErrProviderNotConfigured):
		writeError(w, http.StatusConflict, "provider_not_configured", "Provider is not configured")
	case errors.Is(err, commandservice.ErrProviderActionUnsupported):
		writeError(w, http.StatusConflict, "provider_action_unsupported", "Provider does not support this action")
	case errors.Is(err, commandservice.ErrProviderMappingUnknown):
		writeError(w, http.StatusConflict, "provider_mapping_unknown", "Provider action mapping is not proven")
	case errors.Is(err, commandservice.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusConflict, "idempotency_key_conflict", "idempotency key conflicts with another request")
	case errors.Is(err, commandservice.ErrCommandNotCancellable):
		writeError(w, http.StatusConflict, "command_not_cancellable", "Command is not cancellable")
	case errors.Is(err, commandservice.ErrCapabilityUnsupported):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", "Device capability is not supported")
	case errors.Is(err, commandservice.ErrPayloadInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_capability_payload", "Command payload is invalid")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectservice.ErrInvalidRequest), errors.Is(err, devicecore.ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.Is(err, projectservice.ErrProjectNotFound), errors.Is(err, devicecore.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, projectservice.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "operation is forbidden")
	case errors.Is(err, projectservice.ErrManagerNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, projectservice.ErrManagerInactive):
		writeError(w, http.StatusConflict, "project_manager_inactive", "Project manager is inactive")
	case errors.Is(err, projectservice.ErrWebhookNotConfigured):
		writeError(w, http.StatusConflict, "webhook_not_configured", "Webhook endpoint is not configured")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func defaultProjectMetadata(req *http.Request) projectservice.RequestMetadata {
	return projectservice.RequestMetadata{
		ActorUserID: defaultActorUserID, IPAddress: directPeerIP(req.RemoteAddr), RequestID: httpjson.RequestID(req.Context()),
	}
}

func defaultDeviceMetadata(req *http.Request) deviceservice.RequestMetadata {
	return deviceservice.RequestMetadata{
		ActorType: domain.ActorTypeUser, ActorUserID: defaultActorUserID,
		IPAddress: directPeerIP(req.RemoteAddr), RequestID: httpjson.RequestID(req.Context()),
	}
}

func defaultCommandMetadata(req *http.Request) commandservice.RequestMetadata {
	return commandservice.RequestMetadata{
		ActorType: domain.ActorTypeUser, ActorUserID: defaultActorUserID,
		IPAddress: directPeerIP(req.RemoteAddr), RequestID: httpjson.RequestID(req.Context()),
	}
}

const defaultActorUserID = "00000000-0000-4000-8000-000000000001"

func defaultHumanScope(*http.Request) access.Scope {
	return access.SuperAdmin(defaultActorUserID)
}

func projectCommandMetadata(req *http.Request, projectID string) commandservice.RequestMetadata {
	return commandservice.RequestMetadata{
		ActorType: domain.ActorTypeProject, ActorID: projectID, IPAddress: directPeerIP(req.RemoteAddr),
		RequestID: httpjson.RequestID(req.Context()),
	}
}

func directPeerIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddress))
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddress)
}

type legacyProjectService struct {
	service DeviceService
}

func NewMemoryProjectService(service DeviceService) ProjectService {
	return legacyProjectService{service: service}
}

func (s legacyProjectService) Create(_ context.Context, _ projectservice.Scope, request projectservice.CreateRequest, _ projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	webhookURL := ""
	if request.WebhookURL != nil {
		webhookURL = *request.WebhookURL
	}
	project, err := s.service.CreateProject(devicecore.CreateProjectRequest{Name: request.Name, WebhookURL: webhookURL, IPWhitelist: request.IPWhitelist})
	if err != nil {
		return projectservice.CredentialResult{}, err
	}
	return projectservice.CredentialResult{Project: legacySafeProject(project), APIKey: project.APIKey}, nil
}

func (s legacyProjectService) List(_ context.Context, _ projectservice.Scope, request projectservice.ListRequest) (projectservice.ListResult, error) {
	page, pageSize := request.Page, request.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	all := s.service.ListProjects()
	filtered := make([]projectservice.Project, 0, len(all))
	for _, project := range all {
		if request.Name == nil || project.Name == *request.Name {
			filtered = append(filtered, legacySafeProject(project))
		}
	}
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := min(start+pageSize, len(filtered))
	return projectservice.ListResult{Items: filtered[start:end], Page: page, PageSize: pageSize, Total: int64(len(filtered))}, nil
}

func (s legacyProjectService) Get(_ context.Context, _ projectservice.Scope, projectID string) (projectservice.Project, error) {
	project, err := s.service.GetProject(projectID)
	if err != nil {
		return projectservice.Project{}, err
	}
	return legacySafeProject(project), nil
}

func (s legacyProjectService) Update(_ context.Context, _ projectservice.Scope, projectID string, request projectservice.UpdateRequest, _ projectservice.RequestMetadata) (projectservice.UpdateResult, error) {
	legacyRequest := devicecore.UpdateProjectRequest{Name: request.Name}
	if request.IPWhitelist != nil {
		legacyRequest.IPWhitelist = *request.IPWhitelist
	}
	if request.WebhookURLSet && request.WebhookURL != nil {
		legacyRequest.WebhookURL = request.WebhookURL
	}
	project, err := s.service.UpdateProject(projectID, legacyRequest)
	if err != nil {
		return projectservice.UpdateResult{}, err
	}
	return projectservice.UpdateResult{Project: legacySafeProject(project)}, nil
}

func (legacyProjectService) RotateAPIKey(context.Context, projectservice.Scope, string, projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	return projectservice.CredentialResult{}, projectservice.ErrInvalidRequest
}

func (legacyProjectService) RotateWebhookSecret(context.Context, projectservice.Scope, string, projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	return projectservice.CredentialResult{}, projectservice.ErrWebhookNotConfigured
}

func (legacyProjectService) Transfer(context.Context, projectservice.Scope, string, projectservice.TransferRequest, projectservice.RequestMetadata) (projectservice.Project, error) {
	return projectservice.Project{}, projectservice.ErrInvalidRequest
}

func (s legacyProjectService) AuthenticateAPIKey(_ context.Context, apiKey, _ string) (projectservice.Project, error) {
	project, err := s.service.ProjectByAPIKey(apiKey)
	if err != nil {
		return projectservice.Project{}, projectservice.ErrAuthenticationFailed
	}
	return legacySafeProject(project), nil
}

func legacySafeProject(project devicecore.Project) projectservice.Project {
	var webhookURL *string
	if strings.TrimSpace(project.WebhookURL) != "" {
		value := project.WebhookURL
		webhookURL = &value
	}
	return projectservice.Project{
		ID: project.ID, Name: project.Name, WebhookURL: webhookURL, WebhookConfigured: webhookURL != nil,
		IPWhitelist: append([]string(nil), project.IPWhitelist...), CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}
}

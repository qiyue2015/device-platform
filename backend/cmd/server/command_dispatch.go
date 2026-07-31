package main

import (
	"fmt"
	"strings"

	"github.com/qiyue2015/device-platform/internal/devicecore"
)

type commandDispatchService struct {
	core      *devicecore.Service
	providers cloudProviderRegistry
}

func newCommandDispatchService(core *devicecore.Service, providers cloudProviderRegistry) *commandDispatchService {
	return &commandDispatchService{core: core, providers: providers}
}

func (s *commandDispatchService) CreateProject(req devicecore.CreateProjectRequest) (devicecore.Project, error) {
	return s.core.CreateProject(req)
}

func (s *commandDispatchService) ListProjects() []devicecore.Project {
	return s.core.ListProjects()
}

func (s *commandDispatchService) GetProject(projectID string) (devicecore.Project, error) {
	return s.core.GetProject(projectID)
}

func (s *commandDispatchService) ProjectByAPIKey(apiKey string) (devicecore.Project, error) {
	return s.core.ProjectByAPIKey(apiKey)
}

func (s *commandDispatchService) UpdateProject(projectID string, req devicecore.UpdateProjectRequest) (devicecore.Project, error) {
	return s.core.UpdateProject(projectID, req)
}

func (s *commandDispatchService) CreateDevice(req devicecore.CreateDeviceRequest) (devicecore.Device, error) {
	if strings.TrimSpace(req.AccessType) == devicecore.AccessTypeCloudAPI {
		if strings.TrimSpace(req.ProviderCode) == "" {
			req.ProviderCode = s.providers.DefaultCloudAPIProviderCode()
		}
		if !s.providers.HasProvider(req.ProviderCode) {
			return devicecore.Device{}, fmt.Errorf("%w: unknown cloud provider", devicecore.ErrInvalidArgument)
		}
	}
	return s.core.CreateDevice(req)
}

func (s *commandDispatchService) ListDevices(projectID string) []devicecore.Device {
	return s.core.ListDevices(projectID)
}

func (s *commandDispatchService) GetDevice(projectID, deviceID string) (devicecore.Device, error) {
	return s.core.GetDevice(projectID, deviceID)
}

func (s *commandDispatchService) SetDeviceOnline(projectID, deviceID string, online bool) error {
	return s.core.SetDeviceOnline(projectID, deviceID, online)
}

func (s *commandDispatchService) CreateCommand(req devicecore.CreateCommandRequest) (devicecore.Command, error) {
	return s.core.CreateCommand(req)
}

func (s *commandDispatchService) ListCommands(projectID string) []devicecore.Command {
	return s.core.ListCommands(projectID)
}

func (s *commandDispatchService) GetCommand(projectID, commandID string) (devicecore.Command, error) {
	return s.core.GetCommand(projectID, commandID)
}

func (s *commandDispatchService) CancelCommand(projectID, commandID string) (devicecore.Command, error) {
	return s.core.CancelCommand(projectID, commandID)
}

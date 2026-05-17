package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
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
	command, err := s.core.CreateCommand(req)
	if err != nil {
		return command, err
	}
	if command.Status != devicecore.CommandStatusQueued {
		return command, nil
	}
	device, err := s.core.GetDevice(command.ProjectID, command.DeviceID)
	if err != nil {
		return command, nil
	}
	if device.Adapter != devicecore.AdapterWWTIOTCloudAPI {
		return command, nil
	}
	client, ok := s.providers.WWTIOTClient(device.ProviderCode)
	if !ok {
		client = &wwtiot.Client{}
	}
	return s.dispatchWWTIOT(command, device, client)
}

func (s *commandDispatchService) dispatchWWTIOT(command devicecore.Command, device devicecore.Device, client *wwtiot.Client) (devicecore.Command, error) {
	sent, err := s.core.MarkCommandSent(command.ProjectID, command.ID)
	if err != nil {
		return command, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, dispatchErr := client.SendCommand(ctx, device.ProviderDeviceID, sent.CommandType, sent.Payload)
	attemptStatus := "acked"
	if dispatchErr != nil {
		attemptStatus = "failed"
	}
	recorded, recordErr := s.core.RecordCommandAttempt(sent.ProjectID, sent.ID, devicecore.RecordCommandAttemptRequest{
		Adapter:      devicecore.AdapterWWTIOTCloudAPI,
		Status:       attemptStatus,
		RequestBody:  result.HTTPRequest,
		ResponseBody: result.ResponseBody,
		Error:        errorString(dispatchErr),
	})
	if recordErr == nil {
		sent = recorded
	}
	if dispatchErr != nil {
		failed, failErr := s.core.FailCommand(sent.ProjectID, sent.ID, fmt.Sprintf("wwtiot_dispatch_failed: %s", dispatchErr.Error()), false)
		return preferCommand(failed, sent), errors.Join(recordErr, failErr)
	}
	acked, ackErr := s.core.AckCommand(sent.ProjectID, sent.ID)
	if ackErr != nil {
		return preferCommand(acked, sent), errors.Join(recordErr, ackErr)
	}
	succeeded, successErr := s.core.SucceedCommand(acked.ProjectID, acked.ID, false)
	return preferCommand(succeeded, acked), errors.Join(recordErr, successErr)
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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func preferCommand(primary, fallback devicecore.Command) devicecore.Command {
	if primary.ID != "" {
		return primary
	}
	return fallback
}

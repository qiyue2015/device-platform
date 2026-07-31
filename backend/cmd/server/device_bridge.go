package main

import (
	"context"
	"errors"

	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/httpapi"
)

var errDeviceServiceUnavailable = errors.New("Device service unavailable")

type appDeviceService struct {
	app *app
}

func (s appDeviceService) current() (httpapi.DeviceResourceService, error) {
	service := s.app.deviceResourceService()
	if service == nil {
		return nil, errDeviceServiceUnavailable
	}
	return service, nil
}

func (s appDeviceService) ListDeviceTypes(ctx context.Context) ([]deviceservice.DeviceType, error) {
	service, err := s.current()
	if err != nil {
		return nil, err
	}
	return service.ListDeviceTypes(ctx)
}

func (s appDeviceService) GetDeviceType(ctx context.Context, code string) (deviceservice.DeviceType, error) {
	service, err := s.current()
	if err != nil {
		return deviceservice.DeviceType{}, err
	}
	return service.GetDeviceType(ctx, code)
}

func (s appDeviceService) ListProviders() []deviceservice.Provider {
	service, err := s.current()
	if err != nil {
		return nil
	}
	return service.ListProviders()
}

func (s appDeviceService) Create(ctx context.Context, scope deviceservice.Scope, request deviceservice.CreateRequest, metadata deviceservice.RequestMetadata) (deviceservice.Device, error) {
	service, err := s.current()
	if err != nil {
		return deviceservice.Device{}, err
	}
	return service.Create(ctx, scope, request, metadata)
}

func (s appDeviceService) List(ctx context.Context, scope deviceservice.Scope, request deviceservice.ListRequest) (deviceservice.ListResult, error) {
	service, err := s.current()
	if err != nil {
		return deviceservice.ListResult{}, err
	}
	return service.List(ctx, scope, request)
}

func (s appDeviceService) Get(ctx context.Context, scope deviceservice.Scope, deviceID string) (deviceservice.Device, error) {
	service, err := s.current()
	if err != nil {
		return deviceservice.Device{}, err
	}
	return service.Get(ctx, scope, deviceID)
}

func (s appDeviceService) Update(ctx context.Context, scope deviceservice.Scope, deviceID string, request deviceservice.UpdateRequest, metadata deviceservice.RequestMetadata) (deviceservice.Device, error) {
	service, err := s.current()
	if err != nil {
		return deviceservice.Device{}, err
	}
	return service.Update(ctx, scope, deviceID, request, metadata)
}

var _ httpapi.DeviceResourceService = appDeviceService{}

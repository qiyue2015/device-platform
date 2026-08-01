package main

import (
	"context"
	"errors"

	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpapi"
)

var errCommandServiceUnavailable = errors.New("command service unavailable")

type appCommandService struct {
	app *app
}

func (s appCommandService) current() (httpapi.CommandResourceService, error) {
	service := s.app.commandResourceService()
	if service == nil {
		return nil, errCommandServiceUnavailable
	}
	return service, nil
}

func (s appCommandService) Create(ctx context.Context, scope commandservice.Scope, request commandservice.CreateRequest, metadata commandservice.RequestMetadata) (commandservice.CreateResult, error) {
	service, err := s.current()
	if err != nil {
		return commandservice.CreateResult{}, err
	}
	return service.Create(ctx, scope, request, metadata)
}

func (s appCommandService) List(ctx context.Context, scope commandservice.Scope, request commandservice.ListRequest) (commandservice.ListResult, error) {
	service, err := s.current()
	if err != nil {
		return commandservice.ListResult{}, err
	}
	return service.List(ctx, scope, request)
}

func (s appCommandService) Get(ctx context.Context, scope commandservice.Scope, commandID string) (commandservice.Detail, error) {
	service, err := s.current()
	if err != nil {
		return commandservice.Detail{}, err
	}
	return service.Get(ctx, scope, commandID)
}

func (s appCommandService) Cancel(ctx context.Context, scope commandservice.Scope, commandID string, metadata commandservice.RequestMetadata) (domain.Command, error) {
	service, err := s.current()
	if err != nil {
		return domain.Command{}, err
	}
	return service.Cancel(ctx, scope, commandID, metadata)
}

var _ httpapi.CommandResourceService = appCommandService{}

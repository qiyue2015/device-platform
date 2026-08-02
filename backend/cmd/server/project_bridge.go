package main

import (
	"context"
	"errors"

	"github.com/qiyue2015/device-platform/internal/httpapi"
	"github.com/qiyue2015/device-platform/internal/projectservice"
)

var errProjectServiceUnavailable = errors.New("project service unavailable")

type appProjectService struct {
	app *app
}

func (s appProjectService) current() (httpapi.ProjectService, error) {
	service := s.app.projectService()
	if service == nil {
		return nil, errProjectServiceUnavailable
	}
	return service, nil
}

func (s appProjectService) Create(ctx context.Context, scope projectservice.Scope, request projectservice.CreateRequest, metadata projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.CredentialResult{}, err
	}
	return service.Create(ctx, scope, request, metadata)
}

func (s appProjectService) List(ctx context.Context, scope projectservice.Scope, request projectservice.ListRequest) (projectservice.ListResult, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.ListResult{}, err
	}
	return service.List(ctx, scope, request)
}

func (s appProjectService) Get(ctx context.Context, scope projectservice.Scope, projectID string) (projectservice.Project, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.Project{}, err
	}
	return service.Get(ctx, scope, projectID)
}

func (s appProjectService) Update(ctx context.Context, scope projectservice.Scope, projectID string, request projectservice.UpdateRequest, metadata projectservice.RequestMetadata) (projectservice.UpdateResult, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.UpdateResult{}, err
	}
	return service.Update(ctx, scope, projectID, request, metadata)
}

func (s appProjectService) RotateAPIKey(ctx context.Context, scope projectservice.Scope, projectID string, metadata projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.CredentialResult{}, err
	}
	return service.RotateAPIKey(ctx, scope, projectID, metadata)
}

func (s appProjectService) RotateWebhookSecret(ctx context.Context, scope projectservice.Scope, projectID string, metadata projectservice.RequestMetadata) (projectservice.CredentialResult, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.CredentialResult{}, err
	}
	return service.RotateWebhookSecret(ctx, scope, projectID, metadata)
}

func (s appProjectService) Transfer(ctx context.Context, scope projectservice.Scope, projectID string, request projectservice.TransferRequest, metadata projectservice.RequestMetadata) (projectservice.Project, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.Project{}, err
	}
	return service.Transfer(ctx, scope, projectID, request, metadata)
}

func (s appProjectService) AuthenticateAPIKey(ctx context.Context, apiKey, peerAddress string) (projectservice.Project, error) {
	service, err := s.current()
	if err != nil {
		return projectservice.Project{}, err
	}
	return service.AuthenticateAPIKey(ctx, apiKey, peerAddress)
}

var _ httpapi.ProjectService = appProjectService{}

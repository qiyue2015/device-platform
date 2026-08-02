package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/userservice"
)

type createUserPayload struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type updateUserStatusPayload struct {
	Status domain.UserStatus `json:"status"`
}

func (a *app) handleUsers(w http.ResponseWriter, r *http.Request) error {
	service := a.userService()
	if service == nil {
		return newAPIError(http.StatusServiceUnavailable, "user_service_unavailable", "User service is unavailable")
	}
	scope := a.humanScope(r)
	switch r.Method {
	case http.MethodGet:
		request, err := parseUserListRequest(r.URL.RawQuery)
		if err != nil {
			return mapUserServiceError(err)
		}
		result, err := service.List(r.Context(), scope, request)
		if err != nil {
			return mapUserServiceError(err)
		}
		httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": result.Items}, map[string]any{
			"page": result.Page, "page_size": result.PageSize, "total": result.Total,
		})
		return nil
	case http.MethodPost:
		if r.URL.RawQuery != "" {
			return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
		}
		var payload createUserPayload
		if err := httpjson.DecodeStrict(r.Body, &payload); err != nil {
			return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
		}
		created, err := service.Create(r.Context(), scope, userservice.CreateRequest{
			Email: payload.Email, DisplayName: payload.DisplayName, Password: payload.Password,
		}, a.userRequestMetadata(r))
		if err != nil {
			return mapUserServiceError(err)
		}
		httpjson.Created(w, created)
		return nil
	default:
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (a *app) handleUserByID(w http.ResponseWriter, r *http.Request) error {
	service := a.userService()
	if service == nil {
		return newAPIError(http.StatusServiceUnavailable, "user_service_unavailable", "User service is unavailable")
	}
	userID := strings.TrimPrefix(r.URL.Path, "/v1/users/")
	if userID == "" || strings.Contains(userID, "/") || r.URL.RawQuery != "" {
		return newAPIError(http.StatusNotFound, "not_found", "resource not found")
	}
	scope := a.humanScope(r)
	switch r.Method {
	case http.MethodGet:
		user, err := service.Get(r.Context(), scope, userID)
		if err != nil {
			return mapUserServiceError(err)
		}
		writeOK(w, user)
		return nil
	case http.MethodPatch:
		var payload updateUserStatusPayload
		if err := httpjson.DecodeStrict(r.Body, &payload); err != nil {
			return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
		}
		user, err := service.UpdateStatus(r.Context(), scope, userID, userservice.UpdateStatusRequest{Status: payload.Status}, a.userRequestMetadata(r))
		if err != nil {
			return mapUserServiceError(err)
		}
		writeOK(w, user)
		return nil
	default:
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (a *app) userRequestMetadata(r *http.Request) userservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return userservice.RequestMetadata{ActorUserID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context())}
}

func parseUserListRequest(rawQuery string) (userservice.ListRequest, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return userservice.ListRequest{}, userservice.ErrInvalidRequest
	}
	allowed := map[string]bool{"page": true, "page_size": true, "email": true, "status": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			return userservice.ListRequest{}, userservice.ErrInvalidRequest
		}
	}
	page, err := userListInteger(query, "page", 1)
	if err != nil {
		return userservice.ListRequest{}, err
	}
	pageSize, err := userListInteger(query, "page_size", 20)
	if err != nil || pageSize > 100 {
		return userservice.ListRequest{}, userservice.ErrInvalidRequest
	}
	request := userservice.ListRequest{Page: page, PageSize: pageSize}
	if value := query.Get("email"); value != "" {
		request.Email = &value
	}
	if value := query.Get("status"); value != "" {
		status := domain.UserStatus(value)
		request.Status = &status
	}
	return request, nil
}

func userListInteger(query url.Values, key string, fallback int) (int, error) {
	raw := query.Get(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, userservice.ErrInvalidRequest
	}
	return value, nil
}

func mapUserServiceError(err error) error {
	switch {
	case errors.Is(err, userservice.ErrInvalidRequest):
		return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.Is(err, userservice.ErrForbidden):
		return newAPIError(http.StatusForbidden, "forbidden", "operation is forbidden")
	case errors.Is(err, userservice.ErrUserNotFound):
		return newAPIError(http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, userservice.ErrEmailConflict):
		return newAPIError(http.StatusConflict, "user_email_conflict", "User email already exists")
	case errors.Is(err, userservice.ErrSuperAdminImmutable):
		return newAPIError(http.StatusConflict, "super_admin_immutable", "super administrator is immutable")
	case errors.Is(err, userservice.ErrUserHasManagedProjects):
		return newAPIError(http.StatusConflict, "user_has_managed_projects", "User still manages Projects")
	default:
		return err
	}
}

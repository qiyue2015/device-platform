package commandservice

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func validateScope(scope Scope) (Scope, error) {
	switch scope.Kind {
	case ScopeAdmin:
		if scope.ProjectID != "" {
			return Scope{}, fmt.Errorf("%w: admin scope cannot carry a Project", ErrInvalidRequest)
		}
	case ScopeProject:
		scope.ProjectID = strings.TrimSpace(scope.ProjectID)
		if !validUUID(scope.ProjectID) {
			return Scope{}, fmt.Errorf("%w: Project scope is invalid", ErrInvalidRequest)
		}
	default:
		return Scope{}, fmt.Errorf("%w: scope is invalid", ErrInvalidRequest)
	}
	return scope, nil
}

func validateMetadata(scope Scope, metadata RequestMetadata) (RequestMetadata, error) {
	metadata.ActorID = strings.TrimSpace(metadata.ActorID)
	metadata.RequestID = strings.TrimSpace(metadata.RequestID)
	if metadata.RequestID == "" || len(metadata.RequestID) > 255 {
		return RequestMetadata{}, fmt.Errorf("%w: request_id is required", ErrInvalidRequest)
	}
	if metadata.IPAddress != "" {
		address, err := netip.ParseAddr(strings.TrimSpace(metadata.IPAddress))
		if err != nil || address.Zone() != "" {
			return RequestMetadata{}, fmt.Errorf("%w: ip_address is invalid", ErrInvalidRequest)
		}
		metadata.IPAddress = address.Unmap().String()
	}
	if scope.Kind == ScopeAdmin && metadata.ActorType != domain.ActorTypeAdmin {
		return RequestMetadata{}, fmt.Errorf("%w: admin scope requires an admin actor", ErrInvalidRequest)
	}
	if scope.Kind == ScopeProject && (metadata.ActorType != domain.ActorTypeProject || metadata.ActorID != scope.ProjectID) {
		return RequestMetadata{}, fmt.Errorf("%w: Project scope requires its authenticated Project actor", ErrInvalidRequest)
	}
	return metadata, nil
}

func projectForCreate(scope Scope, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if scope.Kind == ScopeProject {
		if requested != "" {
			return "", fmt.Errorf("%w: Project scope cannot override project_id", ErrInvalidRequest)
		}
		return scope.ProjectID, nil
	}
	if !validUUID(requested) {
		return "", fmt.Errorf("%w: project_id is invalid", ErrInvalidRequest)
	}
	return requested, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 128 {
		return "", fmt.Errorf("%w: idempotency_key is invalid", ErrInvalidRequest)
	}
	return value, nil
}

func normalizeCommandType(value domain.ActionIdentifier) (domain.ActionIdentifier, error) {
	normalized := strings.TrimSpace(string(value))
	if !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) < 1 || utf8.RuneCountInString(normalized) > 128 {
		return "", fmt.Errorf("%w: command_type is invalid", ErrInvalidRequest)
	}
	return domain.ActionIdentifier(normalized), nil
}

func normalizePayload(payload map[string]any) (map[string]any, error) {
	if payload == nil {
		return map[string]any{}, nil
	}
	if len(payload) != 0 {
		return nil, ErrPayloadInvalid
	}
	return map[string]any{}, nil
}

func validateListRequest(scope Scope, request ListRequest) (ListRequest, error) {
	if request.Page == 0 {
		request.Page = 1
	}
	if request.PageSize == 0 {
		request.PageSize = 20
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 || request.Page-1 > math.MaxInt/request.PageSize {
		return ListRequest{}, fmt.Errorf("%w: pagination is invalid", ErrInvalidRequest)
	}
	if scope.Kind == ScopeProject {
		if request.ProjectID != nil {
			return ListRequest{}, fmt.Errorf("%w: Project scope cannot override project_id", ErrInvalidRequest)
		}
		projectID := scope.ProjectID
		request.ProjectID = &projectID
	} else if request.ProjectID != nil && !validUUID(*request.ProjectID) {
		return ListRequest{}, fmt.Errorf("%w: project_id is invalid", ErrInvalidRequest)
	}
	if request.DeviceID != nil && !validUUID(*request.DeviceID) {
		return ListRequest{}, fmt.Errorf("%w: device_id is invalid", ErrInvalidRequest)
	}
	if request.CommandType != nil {
		value, err := normalizeCommandType(*request.CommandType)
		if err != nil || value != *request.CommandType {
			return ListRequest{}, fmt.Errorf("%w: command_type filter is invalid", ErrInvalidRequest)
		}
	}
	if request.Status != nil && !validCommandStatus(*request.Status) {
		return ListRequest{}, fmt.Errorf("%w: status filter is invalid", ErrInvalidRequest)
	}
	return request, nil
}

func validCommandStatus(status domain.CommandStatus) bool {
	switch status {
	case domain.CommandStatusQueued, domain.CommandStatusSent, domain.CommandStatusAcked, domain.CommandStatusSuccess,
		domain.CommandStatusFailed, domain.CommandStatusTimeout, domain.CommandStatusCancelled, domain.CommandStatusUnknown:
		return true
	default:
		return false
	}
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16 && value == strings.ToLower(value)
}

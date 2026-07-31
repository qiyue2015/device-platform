package deviceservice

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var providerDeviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 120 {
		return "", fmt.Errorf("%w: name must contain 1..120 characters", ErrInvalidRequest)
	}
	return value, nil
}

func normalizeProviderDeviceID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !providerDeviceIDPattern.MatchString(value) {
		return "", fmt.Errorf("%w: provider_device_id is invalid", ErrInvalidRequest)
	}
	return value, nil
}

func validWWTIOTConfig(endpoint, userID, userKey string) bool {
	rawEndpoint := strings.TrimSpace(endpoint)
	parsed, err := url.Parse(rawEndpoint)
	if err != nil || rawEndpoint == "" || !parsed.IsAbs() || parsed.Host == "" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	trimmedUserID := strings.TrimSpace(userID)
	return utf8.ValidString(trimmedUserID) && len([]byte(trimmedUserID)) >= 1 && len([]byte(trimmedUserID)) <= 128 &&
		utf8.ValidString(userKey) && len([]byte(userKey)) >= 1 && len([]byte(userKey)) <= 512
}

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

func validateMetadata(metadata RequestMetadata) (RequestMetadata, error) {
	switch metadata.ActorType {
	case domain.ActorTypeAdmin, domain.ActorTypeProject:
	default:
		return RequestMetadata{}, fmt.Errorf("%w: actor_type is invalid", ErrInvalidRequest)
	}
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
	return metadata, nil
}

func validateWriteActor(scope Scope, metadata RequestMetadata) error {
	if scope.Kind == ScopeAdmin && metadata.ActorType != domain.ActorTypeAdmin {
		return fmt.Errorf("%w: admin scope requires an admin actor", ErrInvalidRequest)
	}
	if scope.Kind == ScopeProject && (metadata.ActorType != domain.ActorTypeProject || metadata.ActorID != scope.ProjectID) {
		return fmt.Errorf("%w: Project scope requires its authenticated Project actor", ErrInvalidRequest)
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16 && value == strings.ToLower(value)
}

func validConnectionStatus(value domain.ConnectionStatus) bool {
	switch value {
	case domain.ConnectionStatusUnknown, domain.ConnectionStatusOnline, domain.ConnectionStatusOffline:
		return true
	default:
		return false
	}
}

func validLifecycleStatus(value domain.LifecycleStatus) bool {
	switch value {
	case domain.LifecycleStatusActive, domain.LifecycleStatusDisabled, domain.LifecycleStatusDeleted:
		return true
	default:
		return false
	}
}

func canTransitionLifecycle(from, to domain.LifecycleStatus) bool {
	return from == domain.LifecycleStatusActive && (to == domain.LifecycleStatusDisabled || to == domain.LifecycleStatusDeleted) ||
		from == domain.LifecycleStatusDisabled && (to == domain.LifecycleStatusActive || to == domain.LifecycleStatusDeleted)
}

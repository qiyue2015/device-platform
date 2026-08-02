package main

import (
	"errors"
	"net/http"

	v1 "github.com/qiyue2015/device-platform/internal/api/v1"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	simulatorruntime "github.com/qiyue2015/device-platform/internal/simulator"
)

type simulatorUpdatePayload struct {
	Outcome *domain.SimulatorOutcome `json:"outcome"`
	DelayMS *int                     `json:"delay_ms"`
}

func (a *app) handleSimulator(w http.ResponseWriter, r *http.Request) error {
	if !a.humanScope(r).IsSuperAdmin() {
		return newAPIError(http.StatusForbidden, "forbidden", "operation is forbidden")
	}
	service := a.simulatorService()
	if service == nil {
		legacy := http.NewServeMux()
		gateway.NewHandler(a.gateway).RegisterSimulator(legacy)
		legacy.ServeHTTP(w, r)
		return nil
	}
	if r.URL.Path != "/v1/simulator" {
		return newAPIError(http.StatusNotFound, "not_found", "resource not found")
	}
	if r.URL.RawQuery != "" {
		return newAPIError(http.StatusBadRequest, "invalid_request", "query parameters are not allowed")
	}

	switch r.Method {
	case http.MethodGet:
		config, err := service.Get(r.Context())
		if err != nil {
			return err
		}
		writeOK(w, simulatorResponse(config))
		return nil
	case http.MethodPatch:
		var payload simulatorUpdatePayload
		if err := httpjson.DecodeStrict(r.Body, &payload); err != nil || payload.Outcome == nil || payload.DelayMS == nil {
			return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
		}
		user, ok := userFromRequest(r)
		if !ok {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		config, err := service.Update(r.Context(), simulatorruntime.UpdateRequest{
			Outcome: *payload.Outcome, DelayMS: *payload.DelayMS,
		}, simulatorruntime.RequestMetadata{
			ActorUserID: user.ID, IPAddress: clientIP(r),
			RequestID: httpjson.RequestID(r.Context()),
		})
		if errors.Is(err, simulatorruntime.ErrInvalidRequest) {
			return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
		}
		if err != nil {
			return err
		}
		writeOK(w, simulatorResponse(config))
		return nil
	default:
		w.Header().Set("Allow", "GET, PATCH")
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func simulatorResponse(config domain.SimulatorConfig) v1.SimulatorResponse {
	return v1.SimulatorResponse{
		Outcome: config.Outcome, DelayMS: int(config.Delay.Milliseconds()),
		Version: config.Version, UpdatedAt: config.UpdatedAt.UTC(),
	}
}

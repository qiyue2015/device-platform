package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
)

func registerWebhookAuditRoutes(mux *http.ServeMux, application *app) {
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent != nil {
			handlePersistentEvents(w, r, persistent)
			return
		}
		handleMemoryEvents(w, r, application.webhooks)
	})
	mux.HandleFunc("/v1/events/", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent == nil {
			writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		handlePersistentEventDetail(w, r, persistent)
	})

	mux.HandleFunc("/v1/webhook-deliveries", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent != nil {
			handlePersistentDeliveries(w, r, persistent)
			return
		}
		if r.Method != http.MethodGet {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": application.webhooks.ListDeliveries()})
	})

	mux.HandleFunc("/v1/webhook-deliveries/", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent != nil {
			handlePersistentDeliveryDetail(w, r, persistent)
			return
		}
		handleMemoryDeliveryAction(w, r, application.webhooks)
	})

	mux.HandleFunc("/v1/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent != nil {
			handlePersistentAudits(w, r, persistent)
			return
		}
		handleMemoryAudits(w, r, application.webhooks)
	})
	mux.HandleFunc("/v1/audit-logs/", func(w http.ResponseWriter, r *http.Request) {
		persistent := application.persistentWebhookAuditService()
		if persistent == nil {
			writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		handlePersistentAuditDetail(w, r, persistent)
	})
}

func handlePersistentEvents(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	query, page, pageSize, err := parseWebhookAuditListQuery(r.URL.RawQuery, "project_id", "device_id", "command_id", "event_type")
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	result, err := service.ListEvents(r.Context(), webhookaudit.EventListRequest{
		ProjectID: queryValue(query, "project_id"), DeviceID: queryValue(query, "device_id"),
		CommandID: queryValue(query, "command_id"), EventType: queryValue(query, "event_type"),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writePaginatedWebhookJSON(w, result.Items, result.Page, result.PageSize, result.Total)
}

func handlePersistentEventDetail(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	id, ok := resourceID(r.URL.Path, "/v1/events/")
	if !ok {
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if r.URL.RawQuery != "" {
		writePersistentServiceError(w, webhookaudit.ErrInvalidRequest)
		return
	}
	event, err := service.GetEvent(r.Context(), id)
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writeWebhookJSON(w, http.StatusOK, "ok", event)
}

func handlePersistentDeliveries(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	query, page, pageSize, err := parseWebhookAuditListQuery(r.URL.RawQuery, "project_id", "event_id", "status")
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	result, err := service.ListDeliveries(r.Context(), webhookaudit.DeliveryListRequest{
		ProjectID: queryValue(query, "project_id"), EventID: queryValue(query, "event_id"), Status: queryValue(query, "status"),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writePaginatedWebhookJSON(w, result.Items, result.Page, result.PageSize, result.Total)
}

func handlePersistentDeliveryDetail(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/webhook-deliveries/")
	id, action, hasAction := strings.Cut(path, "/")
	if id == "" || strings.Contains(action, "/") {
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if hasAction {
		if action != "resend" {
			writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		if r.Method != http.MethodPost {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if r.URL.RawQuery != "" || !validEmptyJSONBody(r) {
			writePersistentServiceError(w, webhookaudit.ErrInvalidRequest)
			return
		}
		user, _ := userFromRequest(r)
		delivery, err := service.ReplayDead(r.Context(), id, webhookaudit.ReplayRequest{
			ActorID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
		})
		if err != nil {
			writePersistentServiceError(w, err)
			return
		}
		writeWebhookJSON(w, http.StatusCreated, "created", delivery)
		return
	}
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if r.URL.RawQuery != "" {
		writePersistentServiceError(w, webhookaudit.ErrInvalidRequest)
		return
	}
	delivery, err := service.GetDelivery(r.Context(), id)
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writeWebhookJSON(w, http.StatusOK, "ok", delivery)
}

func handlePersistentAudits(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	query, page, pageSize, err := parseWebhookAuditListQuery(r.URL.RawQuery, "project_id", "actor_type", "action", "result", "resource_type", "resource_id")
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	result, err := service.ListAudits(r.Context(), webhookaudit.AuditListRequest{
		ProjectID: queryValue(query, "project_id"), ActorType: queryValue(query, "actor_type"),
		Action: queryValue(query, "action"), Result: queryValue(query, "result"),
		ResourceType: queryValue(query, "resource_type"), ResourceID: queryValue(query, "resource_id"),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writePaginatedWebhookJSON(w, result.Items, result.Page, result.PageSize, result.Total)
}

func handlePersistentAuditDetail(w http.ResponseWriter, r *http.Request, service *webhookaudit.PersistentService) {
	id, ok := resourceID(r.URL.Path, "/v1/audit-logs/")
	if !ok {
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if r.URL.RawQuery != "" {
		writePersistentServiceError(w, webhookaudit.ErrInvalidRequest)
		return
	}
	audit, err := service.GetAudit(r.Context(), id)
	if err != nil {
		writePersistentServiceError(w, err)
		return
	}
	writeWebhookJSON(w, http.StatusOK, "ok", audit)
}

func handleMemoryEvents(w http.ResponseWriter, r *http.Request, service *webhookaudit.Service) {
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": service.ListEvents()})
}

func handleMemoryDeliveryAction(w http.ResponseWriter, r *http.Request, service *webhookaudit.Service) {
	if r.Method != http.MethodPost {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/webhook-deliveries/")
	id, action, ok := strings.Cut(path, "/")
	if !ok || id == "" || action != "resend" {
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	delivery, err := service.ResendDead(r.Context(), id)
	if err != nil {
		writeWebhookServiceError(w, err)
		return
	}
	auditHTTP(service, r, webhookaudit.AuditRequest{
		Action: "webhook.manual_resend", ActorType: "admin", ProjectID: delivery.ProjectID,
		ResourceType: "webhook_delivery", ResourceID: delivery.ID, Metadata: map[string]any{"event_id": delivery.EventID},
	})
	writeWebhookJSON(w, http.StatusOK, "ok", delivery)
}

func handleMemoryAudits(w http.ResponseWriter, r *http.Request, service *webhookaudit.Service) {
	if r.Method != http.MethodGet {
		writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": service.ListAudits()})
}

func parseWebhookAuditListQuery(rawQuery string, filters ...string) (url.Values, int, int, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, 0, 0, webhookaudit.ErrInvalidRequest
	}
	allowed := map[string]bool{"page": true, "page_size": true}
	for _, filter := range filters {
		allowed[filter] = true
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			return nil, 0, 0, webhookaudit.ErrInvalidRequest
		}
	}
	page, err := positiveWebhookQueryInteger(query, "page", 1)
	if err != nil {
		return nil, 0, 0, err
	}
	pageSize, err := positiveWebhookQueryInteger(query, "page_size", 20)
	if err != nil || pageSize > 100 {
		return nil, 0, 0, webhookaudit.ErrInvalidRequest
	}
	return query, page, pageSize, nil
}

func positiveWebhookQueryInteger(query url.Values, key string, fallback int) (int, error) {
	value, exists := query[key]
	if !exists {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value[0])
	if err != nil || parsed < 1 {
		return 0, webhookaudit.ErrInvalidRequest
	}
	return parsed, nil
}

func queryValue(query url.Values, key string) *string {
	values, ok := query[key]
	if !ok {
		return nil
	}
	value := values[0]
	return &value
}

func resourceID(path, prefix string) (string, bool) {
	id := strings.TrimPrefix(path, prefix)
	return id, id != "" && !strings.Contains(id, "/")
}

func validEmptyJSONBody(r *http.Request) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(body) > 1024 {
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return true
	}
	if body[0] != '{' {
		return false
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(body, &value) == nil && len(value) == 0
}

func writePaginatedWebhookJSON(w http.ResponseWriter, items any, page, pageSize int, total int64) {
	httpjson.WriteWithMeta(w, http.StatusOK, "ok", map[string]any{"items": items}, map[string]any{
		"page": page, "page_size": pageSize, "total": total,
	})
}

func writePersistentServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhookaudit.ErrInvalidRequest):
		writeWebhookError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	case errors.Is(err, webhookaudit.ErrResourceNotFound):
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, webhookaudit.ErrWebhookDeliveryNotDead):
		writeWebhookError(w, http.StatusConflict, "webhook_delivery_not_dead", "webhook delivery is not dead")
	case errors.Is(err, webhookaudit.ErrWebhookNotConfigured):
		writeWebhookError(w, http.StatusConflict, "webhook_not_configured", "webhook is not configured")
	default:
		writeWebhookError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func auditHTTP(service *webhookaudit.Service, r *http.Request, request webhookaudit.AuditRequest) {
	_, _ = service.RecordAudit(withHTTPAuditFields(request, r))
}

func withHTTPAuditFields(request webhookaudit.AuditRequest, r *http.Request) webhookaudit.AuditRequest {
	if request.IPAddress == "" {
		request.IPAddress = clientIP(r)
	}
	if request.UserAgent == "" {
		request.UserAgent = r.UserAgent()
	}
	if request.RequestID == "" {
		request.RequestID = httpjson.RequestID(r.Context())
	}
	return request
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func writeWebhookJSON(w http.ResponseWriter, status int, message string, value any) {
	httpjson.Write(w, status, message, value)
}

func writeWebhookError(w http.ResponseWriter, status int, code, message string) {
	httpjson.Error(w, status, code, message)
}

func writeWebhookServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhookaudit.ErrInvalidArgument):
		writeWebhookError(w, http.StatusBadRequest, "invalid_argument", err.Error())
	case errors.Is(err, webhookaudit.ErrNotFound):
		writeWebhookError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, webhookaudit.ErrNotDeadDelivery):
		writeWebhookError(w, http.StatusBadRequest, "webhook_not_dead", "only dead webhook deliveries can be resent")
	case errors.Is(err, webhookaudit.ErrDeliveryBusy):
		writeWebhookError(w, http.StatusConflict, "webhook_delivery_busy", "webhook delivery is already being processed")
	default:
		writeWebhookError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
)

func registerWebhookAuditRoutes(mux *http.ServeMux, service *webhookaudit.Service) {
	mux.HandleFunc("/v1/projects/webhook-endpoints", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var req webhookaudit.ProjectEndpoint
		if !decodeWebhookJSON(w, r, &req) {
			return
		}
		if err := service.UpsertProject(req); err != nil {
			writeWebhookServiceError(w, err)
			return
		}
		auditHTTP(service, r, webhookaudit.AuditRequest{
			Action:       "project.webhook_configured",
			ActorType:    "admin",
			ProjectID:    req.ProjectID,
			ResourceType: "project",
			ResourceID:   req.ProjectID,
			Metadata:     map[string]any{"has_webhook": req.WebhookURL != ""},
		})
		writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"ok": true})
	})

	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": service.ListEvents()})
		case http.MethodPost:
			var req webhookaudit.CreateEventRequest
			if !decodeWebhookJSON(w, r, &req) {
				return
			}
			event, delivery, err := service.CreateEvent(r.Context(), req)
			if err != nil {
				writeWebhookServiceError(w, err)
				return
			}
			auditHTTP(service, r, webhookaudit.AuditRequest{
				Action:       "event.created",
				ActorType:    "system",
				ProjectID:    event.ProjectID,
				ResourceType: "event",
				ResourceID:   event.ID,
				Metadata:     map[string]any{"event_type": event.EventType},
			})
			writeWebhookJSON(w, http.StatusCreated, "created", map[string]any{"event": event, "webhook_delivery": delivery})
		default:
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	})

	mux.HandleFunc("/v1/webhook-deliveries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": service.ListDeliveries()})
	})

	mux.HandleFunc("/v1/webhook-deliveries/", func(w http.ResponseWriter, r *http.Request) {
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
			Action:       "webhook.manual_resend",
			ActorType:    "admin",
			ProjectID:    delivery.ProjectID,
			ResourceType: "webhook_delivery",
			ResourceID:   delivery.ID,
			Metadata:     map[string]any{"event_id": delivery.EventID},
		})
		writeWebhookJSON(w, http.StatusOK, "ok", delivery)
	})

	mux.HandleFunc("/v1/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeWebhookJSON(w, http.StatusOK, "ok", map[string]any{"items": service.ListAudits()})
		case http.MethodPost:
			var req webhookaudit.AuditRequest
			if !decodeWebhookJSON(w, r, &req) {
				return
			}
			audit, err := service.RecordAudit(withHTTPAuditFields(req, r))
			if err != nil {
				writeWebhookServiceError(w, err)
				return
			}
			writeWebhookJSON(w, http.StatusCreated, "created", audit)
		default:
			writeWebhookError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	})
}

func auditHTTP(service *webhookaudit.Service, r *http.Request, req webhookaudit.AuditRequest) {
	_, _ = service.RecordAudit(withHTTPAuditFields(req, r))
}

func withHTTPAuditFields(req webhookaudit.AuditRequest, r *http.Request) webhookaudit.AuditRequest {
	if req.IPAddress == "" {
		req.IPAddress = clientIP(r)
	}
	if req.UserAgent == "" {
		req.UserAgent = r.UserAgent()
	}
	if req.RequestID == "" {
		req.RequestID = r.Header.Get("X-Request-ID")
	}
	return req
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		ip, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(ip)
	}
	host, _, ok := strings.Cut(r.RemoteAddr, ":")
	if ok {
		return host
	}
	return r.RemoteAddr
}

func decodeWebhookJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeWebhookError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return false
	}
	return true
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

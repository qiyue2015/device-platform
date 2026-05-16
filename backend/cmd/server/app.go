package main

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/httpapi"
)

type app struct {
	cfg     config
	logger  *slog.Logger
	devices *devicecore.Service
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

func newApp(cfg config, logger *slog.Logger) *app {
	return newAppWithDeviceService(cfg, logger, devicecore.NewService())
}

func newAppWithDeviceService(cfg config, logger *slog.Logger, devices *devicecore.Service) *app {
	return &app{cfg: cfg, logger: logger, devices: devices}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", a.handle(a.handleHealth))
	mux.HandleFunc("/readyz", a.handle(a.handleReady))

	mux.HandleFunc("/api/auth/login", a.handle(a.handleLogin))
	mux.HandleFunc("/api/auth/refresh", a.handle(a.requireBearer(a.handleRefresh)))
	mux.HandleFunc("/api/auth/logout", a.handle(a.handleLogout))
	mux.HandleFunc("/api/me", a.handle(a.requireBearer(a.handleLegacyMe)))
	mux.HandleFunc("/api/me/menu", a.handle(a.requireBearer(a.handleLegacyMenu)))

	mux.HandleFunc("/v1/auth/login", a.handle(a.handleLogin))
	mux.HandleFunc("/v1/auth/refresh", a.handle(a.requireBearer(a.handleRefresh)))
	mux.HandleFunc("/v1/auth/logout", a.handle(a.handleLogout))
	mux.HandleFunc("/v1/auth/me", a.handle(a.requireBearer(a.handleMe)))
	mux.HandleFunc("/v1/auth/menu", a.handle(a.requireBearer(a.handleMenu)))

	mux.HandleFunc("/v1/admin/", a.handle(a.requireBearer(a.handleAdminPlaceholder)))
	mux.Handle("/v1/open/", httpapi.NewOpenRouter(a.devices))
	mux.Handle("/v1/", a.requireBearerHandler(httpapi.NewRouter(a.devices)))

	return withRequestLogging(a.logger, withCORS(mux))
}

func (a *app) handle(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			handleError(w, a.logger, err)
		}
	}
}

func (a *app) requireBearer(next handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		token, ok := strings.CutPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)
		if !ok {
			token = ""
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.cfg.AdminAccessToken)) != 1 {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		return next(w, r)
	}
}

func (a *app) requireBearerHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.handle(a.requireBearer(func(w http.ResponseWriter, r *http.Request) error {
			next.ServeHTTP(w, r)
			return nil
		}))(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Project-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
		)
		next.ServeHTTP(w, r)
	})
}

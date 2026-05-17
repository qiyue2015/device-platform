package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/httpapi"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
)

type app struct {
	cfg            config
	logger         *slog.Logger
	db             *sql.DB
	auth           authenticator
	deviceService  *devicecore.Service
	commandRouter  httpapi.DeviceService
	cloudProviders cloudProviderRegistry
	gateway        *gateway.Service
	webhooks       *webhookaudit.Service
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

func newApp(cfg config, logger *slog.Logger) (*app, error) {
	var db *sql.DB
	var auth authenticator
	if cfg.isInstalled() {
		var err error
		db, err = sql.Open("postgres", cfg.DatabaseURL)
		if err != nil {
			return nil, err
		}
		if err := validateRuntimeDependencies(context.Background(), db, cfg.RedisURL); err != nil {
			_ = db.Close()
			return nil, err
		}
		auth = newDBAuthenticator(db, cfg.JWTSecret)
	}
	service := devicecore.NewService()
	simulatorGateway := gateway.NewSimulatorGateway(gateway.ModeConfig{})
	gatewayService := gateway.NewService(simulatorGateway, gateway.ServiceConfig{})
	webhookService := webhookaudit.NewService(http.DefaultClient)
	cloudProviders := newCloudProviderRegistry(cfg)
	startWebhookWorker(context.Background(), webhookService)
	return newAppWithServices(cfg, logger, db, auth, service, gatewayService, webhookService, cloudProviders), nil
}

func newAppWithDeviceService(cfg config, logger *slog.Logger, service *devicecore.Service) *app {
	simulatorGateway := gateway.NewSimulatorGateway(gateway.ModeConfig{})
	gatewayService := gateway.NewService(simulatorGateway, gateway.ServiceConfig{})
	webhookService := webhookaudit.NewService(http.DefaultClient)
	cloudProviders := newCloudProviderRegistry(cfg)
	secret := cfg.JWTSecret
	if secret == "" {
		secret = defaultMemoryJWTSecret
	}
	auth, _ := newMemoryAuthenticator("admin@test.local", "Test Admin", "test-admin-password", secret)
	return newAppWithServices(cfg, logger, nil, auth, service, gatewayService, webhookService, cloudProviders)
}

func newAppWithServices(cfg config, logger *slog.Logger, db *sql.DB, auth authenticator, service *devicecore.Service, gatewayService *gateway.Service, webhookService *webhookaudit.Service, cloudProviders cloudProviderRegistry) *app {
	commandRouter := httpapi.DeviceService(service)
	if len(cloudProviders.List()) > 0 {
		commandRouter = newCommandDispatchService(service, cloudProviders)
	}
	return &app{
		cfg:            cfg,
		logger:         logger,
		db:             db,
		auth:           auth,
		deviceService:  service,
		commandRouter:  commandRouter,
		cloudProviders: cloudProviders,
		gateway:        gatewayService,
		webhooks:       webhookService,
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", a.handle(a.handleHealth))
	mux.HandleFunc("/readyz", a.handle(a.handleReady))
	mux.HandleFunc("/setup/status", a.handle(a.handleSetupStatus))
	mux.HandleFunc("/setup/test-db", a.handle(a.handleSetupTestDB))
	mux.HandleFunc("/setup/test-redis", a.handle(a.handleSetupTestRedis))
	mux.HandleFunc("/setup/install", a.handle(a.handleSetupInstall))

	mux.HandleFunc("/v1/auth/login", a.handle(a.handleLogin))
	mux.HandleFunc("/v1/auth/refresh", a.handle(a.requireBearer(a.handleRefresh)))
	mux.HandleFunc("/v1/auth/logout", a.handle(a.handleLogout))
	mux.HandleFunc("/v1/auth/me", a.handle(a.requireBearer(a.handleMe)))
	mux.HandleFunc("/v1/auth/menu", a.handle(a.requireBearer(a.handleMenu)))
	mux.HandleFunc("/v1/cloud-providers", a.handle(a.requireBearer(a.handleCloudProviders)))

	mux.HandleFunc("/v1/admin/", a.handle(a.requireBearer(a.handleAdminPlaceholder)))
	openRouter := httpapi.NewOpenRouterWithHooks(a.commandRouter, httpapi.RouterHooks{
		OnCommandCreated: a.recordCommandCreated,
	})
	mux.Handle("/v1/open/", openRouter)
	protectedV1 := http.NewServeMux()
	registerWebhookAuditRoutes(protectedV1, a.webhooks)
	gateway.NewHandler(a.gateway).RegisterSimulator(protectedV1)
	protectedV1.Handle("/v1/", httpapi.NewRouterWithHooks(a.commandRouter, httpapi.RouterHooks{
		OnCommandCreated: a.recordCommandCreated,
	}))
	mux.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/open/") {
			openRouter.ServeHTTP(w, r)
			return
		}
		a.requireBearerHandler(protectedV1).ServeHTTP(w, r)
	}))

	return withRequestLogging(a.logger, withCORS(mux))
}

func (a *app) recordCommandCreated(r *http.Request, command devicecore.Command) {
	payload := map[string]any{
		"command_type":    command.CommandType,
		"delivery_policy": string(command.DeliveryPolicy),
		"status":          string(command.Status),
		"reason":          command.Reason,
	}
	_, _, _ = a.webhooks.CreateEvent(r.Context(), webhookaudit.CreateEventRequest{
		ProjectID: command.ProjectID,
		DeviceID:  command.DeviceID,
		CommandID: command.ID,
		EventType: "command.created",
		Source:    "device-platform",
		Payload:   payload,
	})
	actorType := "admin"
	if strings.HasPrefix(r.URL.Path, "/v1/open/") {
		actorType = "open-api"
	}
	_, _ = a.webhooks.RecordAudit(withHTTPAuditFields(webhookaudit.AuditRequest{
		Action:       "command.created",
		ActorType:    actorType,
		ProjectID:    command.ProjectID,
		ResourceType: "device_command",
		ResourceID:   command.ID,
		Metadata:     payload,
	}, r))
}

func validateRuntimeDependencies(ctx context.Context, db *sql.DB, redisURL string) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("database unavailable after installation: %w", err)
	}
	if err := testRedisConnection(ctx, redisURL); err != nil {
		return fmt.Errorf("redis unavailable after installation: %w", err)
	}
	return nil
}

func startWebhookWorker(ctx context.Context, service *webhookaudit.Service) {
	ticker := time.NewTicker(500 * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				service.RetryDue(ctx)
			}
		}
	}()
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
		if a.auth == nil || token == "" {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		user, err := a.auth.ParseToken(token)
		if err != nil {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		ctx := context.WithValue(r.Context(), currentUserContextKey{}, user)
		return next(w, r.WithContext(ctx))
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

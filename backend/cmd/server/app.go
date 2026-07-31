package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/commandworker"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/httpapi"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	simulatorruntime "github.com/qiyue2015/device-platform/internal/simulator"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
	"github.com/qiyue2015/device-platform/internal/webhookworker"
)

type app struct {
	runtimeMu      sync.RWMutex
	cfg            config
	logger         *slog.Logger
	db             *sql.DB
	auth           authenticator
	deviceService  *devicecore.Service
	commandRouter  httpapi.DeviceService
	projects       httpapi.ProjectService
	devices        httpapi.DeviceResourceService
	commands       httpapi.CommandResourceService
	simulator      *simulatorruntime.Service
	cloudProviders cloudProviderRegistry
	gateway        *gateway.Service
	webhooks       *webhookaudit.Service
	webhookAudit   *webhookaudit.PersistentService
	workerMu       sync.Mutex
	commandCancel  context.CancelFunc
	commandDone    chan struct{}
	webhookCancel  context.CancelFunc
	webhookDone    chan struct{}
	backgroundStop context.CancelFunc
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

type commandWorkerRunner interface {
	Run(context.Context, commandworker.ErrorReporter)
}

type webhookWorkerRunner interface {
	Run(context.Context, webhookworker.ErrorReporter)
}

func newApp(cfg config, logger *slog.Logger) (*app, error) {
	cfg.Installed = cfg.isInstalled()
	var db *sql.DB
	var auth authenticator
	var projects httpapi.ProjectService
	var devices *deviceservice.Service
	var commands httpapi.CommandResourceService
	if cfg.Installed {
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
		store := repository.NewPostgresStore(db)
		projects, err = projectservice.New(store, projectservice.Config{
			EncryptionKeys: map[int][]byte{1: cfg.WebhookSecretEncryptionKey}, ActiveEncryptionKeyVersion: 1,
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Project service: %w", err)
		}
		devices, err = deviceservice.New(store, deviceServiceConfig(cfg))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Device service: %w", err)
		}
		commands, err = commandservice.New(store, commandServiceConfig(devices))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Command service: %w", err)
		}
	}
	service := devicecore.NewService()
	if projects == nil {
		projects = httpapi.NewMemoryProjectService(service)
	}
	simulatorGateway := gateway.NewSimulatorGateway(gateway.ModeConfig{})
	gatewayService := gateway.NewService(simulatorGateway, gateway.ServiceConfig{})
	webhookService := webhookaudit.NewService(http.DefaultClient)
	cloudProviders := newCloudProviderRegistry(cfg)
	application := newAppWithServices(cfg, logger, db, auth, service, gatewayService, webhookService, cloudProviders, projects, devices)
	application.commands = commands
	if db != nil {
		store := repository.NewPostgresStore(db)
		worker, err := newPersistentCommandWorker(store, cloudProviders)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Command worker: %w", err)
		}
		webhookWorker, err := newPersistentWebhookWorker(store, projects, cfg)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Webhook worker: %w", err)
		}
		application.replaceCommandWorker(worker)
		application.replaceWebhookWorker(webhookWorker)
	} else {
		backgroundContext, backgroundStop := context.WithCancel(context.Background())
		application.backgroundStop = backgroundStop
		startWebhookWorker(backgroundContext, webhookService)
	}
	return application, nil
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
	application := newAppWithServices(cfg, logger, nil, auth, service, gatewayService, webhookService, cloudProviders, httpapi.NewMemoryProjectService(service), nil)
	return application
}

func newAppWithServices(cfg config, logger *slog.Logger, db *sql.DB, auth authenticator, service *devicecore.Service, gatewayService *gateway.Service, webhookService *webhookaudit.Service, cloudProviders cloudProviderRegistry, projects httpapi.ProjectService, devices httpapi.DeviceResourceService) *app {
	commandRouter := httpapi.DeviceService(service)
	if len(cloudProviders.List()) > 0 {
		commandRouter = newCommandDispatchService(service, cloudProviders)
	}
	var simulatorService *simulatorruntime.Service
	var webhookAuditService *webhookaudit.PersistentService
	if db != nil {
		store := repository.NewPostgresStore(db)
		simulatorService = simulatorruntime.NewService(store, nil)
		webhookAuditService = webhookaudit.NewPersistentService(store)
	}
	return &app{
		cfg:            cfg,
		logger:         logger,
		db:             db,
		auth:           auth,
		deviceService:  service,
		commandRouter:  commandRouter,
		projects:       projects,
		devices:        devices,
		simulator:      simulatorService,
		cloudProviders: cloudProviders,
		gateway:        gatewayService,
		webhooks:       webhookService,
		webhookAudit:   webhookAuditService,
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
	mux.HandleFunc("/v1/auth/logout", a.handle(a.requireBearer(a.handleLogout)))
	mux.HandleFunc("/v1/auth/me", a.handle(a.requireBearer(a.handleMe)))
	mux.HandleFunc("/v1/auth/menu", a.handle(a.requireBearer(a.handleMenu)))
	mux.HandleFunc("/v1/cloud-providers", a.handle(a.requireBearer(a.handleCloudProviders)))
	mux.HandleFunc("/v1/provider-callbacks/", a.handle(a.handleProviderCallback))

	mux.HandleFunc("/v1/admin/", a.handle(a.requireBearer(a.handleAdminPlaceholder)))
	projectBridge := appProjectService{app: a}
	deviceBridge := appDeviceService{app: a}
	commandBridge := appCommandService{app: a}
	routerHooks := httpapi.RouterHooks{
		OnCommandCreated: a.recordCommandCreated,
		ProjectMetadata:  a.projectRequestMetadata,
		DeviceMetadata:   a.deviceRequestMetadata,
		CommandMetadata:  a.commandRequestMetadata,
	}
	legacyOpenRouter := httpapi.NewOpenRouterWithResourceServices(a.commandRouter, projectBridge, nil, routerHooks)
	resourceOpenRouter := httpapi.NewOpenRouterWithDomainServices(a.commandRouter, projectBridge, deviceBridge, commandBridge, routerHooks)
	openRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.deviceResourceService() == nil {
			legacyOpenRouter.ServeHTTP(w, r)
			return
		}
		resourceOpenRouter.ServeHTTP(w, r)
	})
	mux.Handle("/v1/open/", openRouter)
	protectedV1 := http.NewServeMux()
	registerWebhookAuditRoutes(protectedV1, a)
	protectedV1.HandleFunc("/v1/simulator", a.handle(a.handleSimulator))
	protectedV1.HandleFunc("/v1/simulator/gateway", a.handle(a.handleSimulator))
	legacyV1Router := httpapi.NewRouterWithResourceServices(a.commandRouter, projectBridge, nil, routerHooks)
	resourceV1Router := httpapi.NewRouterWithDomainServices(a.commandRouter, projectBridge, deviceBridge, commandBridge, routerHooks)
	protectedV1.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.deviceResourceService() == nil {
			legacyV1Router.ServeHTTP(w, r)
			return
		}
		resourceV1Router.ServeHTTP(w, r)
	}))
	mux.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/open/") {
			openRouter.ServeHTTP(w, r)
			return
		}
		a.requireBearerHandler(protectedV1).ServeHTTP(w, r)
	}))

	return httpjson.WithRequestID(withRequestLogging(a.logger, withCORS(mux)))
}

func (a *app) runtimeConfig() config {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.cfg
}

func (a *app) runtimeInstalled() bool {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.cfg.Installed
}

func (a *app) authenticationService() authenticator {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.auth
}

func (a *app) replaceRuntime(cfg config, db *sql.DB, auth authenticator, projects httpapi.ProjectService, devices httpapi.DeviceResourceService, commands httpapi.CommandResourceService) *sql.DB {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	previousDB := a.db
	a.cfg = cfg
	a.db = db
	a.auth = auth
	a.projects = projects
	a.devices = devices
	a.commands = commands
	a.simulator = nil
	a.webhookAudit = nil
	if db != nil {
		store := repository.NewPostgresStore(db)
		a.simulator = simulatorruntime.NewService(store, nil)
		a.webhookAudit = webhookaudit.NewPersistentService(store)
	}
	return previousDB
}

func (a *app) simulatorService() *simulatorruntime.Service {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.simulator
}

func (a *app) persistentWebhookAuditService() *webhookaudit.PersistentService {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.webhookAudit
}

func newPersistentCommandWorker(store commandworker.Store, providers cloudProviderRegistry) (*commandworker.Worker, error) {
	client, ok := providers.WWTIOTClient(domain.ProviderCodeWWTIOT)
	if !ok {
		return nil, fmt.Errorf("WWTIOT adapter is not registered")
	}
	return commandworker.New(store, commandworker.Config{Adapters: []commandworker.AdapterRegistration{
		{
			ProviderCode: domain.ProviderCodeWWTIOT, AdapterCode: domain.AdapterWWTIOTCloudAPI,
			Adapter: client, ResultSource: domain.EventSourceSystem,
		},
		{
			ProviderCode: domain.ProviderCodeSimulator, AdapterCode: domain.AdapterSimulator,
			Adapter: simulatorruntime.NewAdapter(), ResultSource: domain.EventSourceSimulator,
			ClaimSnapshot: simulatorruntime.ClaimSnapshot,
		},
	}})
}

func newPersistentWebhookWorker(store webhookworker.Store, projects httpapi.ProjectService, cfg config) (*webhookworker.Worker, error) {
	resolver, ok := projects.(webhookworker.SecretResolver)
	if !ok {
		return nil, fmt.Errorf("Project service cannot resolve Webhook secret versions")
	}
	allowlist, err := webhookworker.ParseEgressAllowlist(cfg.WebhookEgressAllowlist)
	if err != nil {
		return nil, err
	}
	timeout := cfg.WebhookRequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client, err := webhookworker.NewSecureHTTPClient(timeout, allowlist)
	if err != nil {
		return nil, err
	}
	interval := cfg.WebhookWorkerInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	leaseDuration := cfg.WebhookLeaseDuration
	if leaseDuration <= timeout {
		leaseDuration = timeout + 5*time.Second
	}
	return webhookworker.New(store, resolver, webhookworker.Config{
		WorkerID: "webhook-dispatcher", PollInterval: interval, LeaseDuration: leaseDuration,
		MaxAttempts: cfg.WebhookMaxAttempts, RetrySchedule: cfg.WebhookRetrySchedule, Client: client,
	})
}

func (a *app) replaceCommandWorker(worker commandWorkerRunner) {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	previousCancel, previousDone := a.commandCancel, a.commandDone
	a.commandCancel, a.commandDone = nil, nil
	if previousCancel != nil {
		previousCancel()
	}
	if previousDone != nil {
		<-previousDone
	}
	if worker == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.commandCancel, a.commandDone = cancel, done
	go func() {
		defer close(done)
		worker.Run(ctx, func(err error) {
			a.logger.Error("persistent Command worker failed", slog.String("error", err.Error()))
		})
	}()
}

func (a *app) replaceWebhookWorker(worker webhookWorkerRunner) {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	previousCancel, previousDone := a.webhookCancel, a.webhookDone
	a.webhookCancel, a.webhookDone = nil, nil
	if previousCancel != nil {
		previousCancel()
	}
	if previousDone != nil {
		<-previousDone
	}
	if worker == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.webhookCancel, a.webhookDone = cancel, done
	go func() {
		defer close(done)
		worker.Run(ctx, func(err error) {
			a.logger.Error("persistent Webhook worker failed", slog.String("error", err.Error()))
		})
	}()
}

func (a *app) stopMemoryWebhookWorker() {
	if a.backgroundStop != nil {
		a.backgroundStop()
		a.backgroundStop = nil
	}
}

func (a *app) close() error {
	a.stopMemoryWebhookWorker()
	a.replaceCommandWorker(nil)
	a.replaceWebhookWorker(nil)
	if a.gateway != nil {
		a.gateway.Stop()
	}
	a.runtimeMu.Lock()
	db := a.db
	a.db = nil
	a.runtimeMu.Unlock()
	if db != nil {
		return db.Close()
	}
	return nil
}

func (a *app) projectService() httpapi.ProjectService {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.projects
}

func (a *app) setProjectService(service httpapi.ProjectService) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.projects = service
}

func (a *app) deviceResourceService() httpapi.DeviceResourceService {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.devices
}

func (a *app) setDeviceResourceService(service httpapi.DeviceResourceService) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.devices = service
}

func (a *app) commandResourceService() httpapi.CommandResourceService {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.commands
}

func (a *app) setCommandResourceService(service httpapi.CommandResourceService) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.commands = service
}

func (a *app) projectRequestMetadata(r *http.Request) projectservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return projectservice.RequestMetadata{
		ActorType: domain.ActorTypeAdmin, ActorID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func (a *app) deviceRequestMetadata(r *http.Request) deviceservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return deviceservice.RequestMetadata{
		ActorType: domain.ActorTypeAdmin, ActorID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func (a *app) commandRequestMetadata(r *http.Request) commandservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return commandservice.RequestMetadata{
		ActorType: domain.ActorTypeAdmin, ActorID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func deviceServiceConfig(cfg config) deviceservice.Config {
	return deviceservice.Config{
		WWTIOTEndpoint: cfg.WWTIOTAPIURL, WWTIOTUserID: cfg.WWTIOTUserID, WWTIOTUserKey: cfg.WWTIOTUserKey,
	}
}

func commandServiceConfig(devices *deviceservice.Service) commandservice.Config {
	providers := devices.ListProviders()
	registry := make([]domain.Provider, 0, len(providers))
	for _, provider := range providers {
		registry = append(registry, domain.Provider{
			Code: provider.Code, Name: provider.Name, AccessType: provider.AccessType,
			TransportProtocol: provider.TransportProtocol, Adapter: provider.Adapter,
			IntegrationStatus: provider.IntegrationStatus,
		})
	}
	return commandservice.Config{Providers: registry}
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
	if err := storage.ValidateFrozenContracts(pingCtx, db); err != nil {
		return fmt.Errorf("database contract validation failed: %w", err)
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
		auth := a.authenticationService()
		if auth == nil || token == "" {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		user, err := auth.ParseToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, errAuthDependencyUnavailable) {
				return newAPIError(http.StatusServiceUnavailable, "auth_dependency_unavailable", "authentication service unavailable")
			}
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
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
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
			slog.String("request_id", httpjson.RequestID(r.Context())),
		)
		next.ServeHTTP(w, r)
	})
}

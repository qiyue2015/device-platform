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

	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/commandworker"
	"github.com/qiyue2015/device-platform/internal/devicecore"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/directdevice/omni"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/gateway"
	"github.com/qiyue2015/device-platform/internal/httpapi"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	simulatorruntime "github.com/qiyue2015/device-platform/internal/simulator"
	"github.com/qiyue2015/device-platform/internal/storage"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
	"github.com/qiyue2015/device-platform/internal/userservice"
	"github.com/qiyue2015/device-platform/internal/webhookaudit"
	"github.com/qiyue2015/device-platform/internal/webhookworker"
)

type app struct {
	runtimeMu      sync.RWMutex
	cfg            config
	recoveryNeeded bool
	runtimeFailed  bool
	logger         *slog.Logger
	fatal          chan error
	db             *sql.DB
	auth           authenticator
	deviceService  *devicecore.Service
	commandRouter  httpapi.DeviceService
	projects       httpapi.ProjectService
	devices        httpapi.DeviceResourceService
	commands       httpapi.CommandResourceService
	users          *userservice.Service
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
	omniRuntime    omniRuntimeRunner
	omniCancel     context.CancelFunc
	omniDone       chan struct{}
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

type commandWorkerRunner interface {
	Run(context.Context, commandworker.ErrorReporter)
}

type webhookWorkerRunner interface {
	Run(context.Context, webhookworker.ErrorReporter)
}

type omniRuntimeRunner interface {
	Configured() bool
	Run(context.Context, func(error)) error
	Close() error
}

type runtimeSnapshot struct {
	cfg            config
	db             *sql.DB
	auth           authenticator
	projects       httpapi.ProjectService
	devices        httpapi.DeviceResourceService
	commands       httpapi.CommandResourceService
	users          *userservice.Service
	simulator      *simulatorruntime.Service
	webhookAudit   *webhookaudit.PersistentService
	cloudProviders cloudProviderRegistry
	commandWorker  commandWorkerRunner
	webhookWorker  webhookWorkerRunner
	omniRuntime    omniRuntimeRunner
}

func newApp(cfg config, logger *slog.Logger) (*app, error) {
	cfg.Installed = cfg.isInstalled()
	var db *sql.DB
	var auth authenticator
	var projects httpapi.ProjectService
	var devices *deviceservice.Service
	var commands httpapi.CommandResourceService
	var users *userservice.Service
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
		users, err = userservice.New(store, userservice.Config{})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize User service: %w", err)
		}
	}
	cloudProviders := newCloudProviderRegistry(cfg)
	application := newAppWithServices(cfg, logger, db, auth, nil, nil, nil, cloudProviders, projects, devices)
	application.commands = commands
	application.users = users
	if db != nil {
		store := repository.NewPostgresStore(db)
		omniRuntime, err := newOmniRuntime(store, cfg)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize Omni runtime: %w", err)
		}
		worker, err := newPersistentCommandWorker(store, cloudProviders, omniRuntime.Adapter())
		if err != nil {
			_ = omniRuntime.Close()
			_ = db.Close()
			return nil, fmt.Errorf("initialize Command worker: %w", err)
		}
		webhookWorker, err := newPersistentWebhookWorker(store, projects, cfg)
		if err != nil {
			_ = omniRuntime.Close()
			_ = db.Close()
			return nil, fmt.Errorf("initialize Webhook worker: %w", err)
		}
		application.replaceOmniRuntime(omniRuntime)
		application.replaceCommandWorker(worker)
		application.replaceWebhookWorker(webhookWorker)
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
	var commandRouter httpapi.DeviceService
	if service != nil {
		commandRouter = service
		if len(cloudProviders.List()) > 0 {
			commandRouter = newCommandDispatchService(service, cloudProviders)
		}
	}
	var simulatorService *simulatorruntime.Service
	var webhookAuditService *webhookaudit.PersistentService
	var userService *userservice.Service
	if db != nil {
		store := repository.NewPostgresStore(db)
		simulatorService = simulatorruntime.NewService(store, nil)
		webhookAuditService = webhookaudit.NewPersistentService(store)
		userService, _ = userservice.New(store, userservice.Config{})
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
		users:          userService,
		simulator:      simulatorService,
		cloudProviders: cloudProviders,
		gateway:        gatewayService,
		webhooks:       webhookService,
		webhookAudit:   webhookAuditService,
		fatal:          make(chan error, 1),
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
	mux.HandleFunc("/v1/cloud-providers", a.handle(a.requireBearer(a.handleCloudProviders)))
	mux.HandleFunc("/v1/provider-callbacks/", a.handle(a.handleProviderCallback))

	projectBridge := appProjectService{app: a}
	deviceBridge := appDeviceService{app: a}
	commandBridge := appCommandService{app: a}
	routerHooks := httpapi.RouterHooks{
		OnCommandCreated: a.recordCommandCreated,
		ProjectMetadata:  a.projectRequestMetadata,
		DeviceMetadata:   a.deviceRequestMetadata,
		CommandMetadata:  a.commandRequestMetadata,
		HumanScope:       a.humanScope,
	}
	legacyOpenRouter := httpapi.NewOpenRouterWithResourceServices(a.commandRouter, projectBridge, nil, routerHooks)
	resourceOpenRouter := httpapi.NewOpenRouterWithDomainServices(a.commandRouter, projectBridge, deviceBridge, commandBridge, routerHooks)
	openRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.runtimeInstalled() {
			handleError(w, a.logger, newAPIError(http.StatusServiceUnavailable, "setup_required", "system setup is required"))
			return
		}
		if a.deviceResourceService() == nil {
			legacyOpenRouter.ServeHTTP(w, r)
			return
		}
		resourceOpenRouter.ServeHTTP(w, r)
	})
	mux.Handle("/v1/open/", openRouter)
	protectedV1 := http.NewServeMux()
	protectedV1.HandleFunc("/v1/users", a.handle(a.handleUsers))
	protectedV1.HandleFunc("/v1/users/", a.handle(a.handleUserByID))
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
		if !a.runtimeInstalled() {
			handleError(w, a.logger, a.runtimeUnavailableError())
			return
		}
		a.requireBearerHandler(protectedV1).ServeHTTP(w, r)
	}))

	return httpjson.WithRequestID(withRequestLogging(a.logger, withCORS(a.requireAvailableRuntime(mux))))
}

func (a *app) requireAvailableRuntime(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			if err := a.runtimeUnavailableError(); err != nil {
				handleError(w, a.logger, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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

func (a *app) runtimeUnavailableError() error {
	a.runtimeMu.RLock()
	installed := a.cfg.Installed
	recoveryNeeded := a.recoveryNeeded
	runtimeFailed := a.runtimeFailed
	a.runtimeMu.RUnlock()
	if recoveryNeeded || installRecoveryExists() {
		return newAPIError(http.StatusServiceUnavailable, "setup_recovery_required", "installation recovery is required")
	}
	if !installed && installLockExists() {
		return newAPIError(http.StatusServiceUnavailable, "setup_restart_required", "process restart is required")
	}
	if installed {
		if runtimeFailed {
			return newAPIError(http.StatusServiceUnavailable, "provider_runtime_unavailable", "required Provider runtime is unavailable")
		}
		return nil
	}
	return newAPIError(http.StatusServiceUnavailable, "setup_required", "system setup is required")
}

func (a *app) setRecoveryNeeded(value bool) {
	a.runtimeMu.Lock()
	a.recoveryNeeded = value
	a.runtimeMu.Unlock()
}

func (a *app) signalFatal(err error) {
	a.setRecoveryNeeded(true)
	select {
	case a.fatal <- err:
	default:
	}
}

func (a *app) signalRuntimeFatal(err error) {
	a.runtimeMu.Lock()
	a.runtimeFailed = true
	a.runtimeMu.Unlock()
	select {
	case a.fatal <- err:
	default:
	}
}

func (a *app) fatalErrors() <-chan error {
	return a.fatal
}

func (a *app) authenticationService() authenticator {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.auth
}

func (a *app) userService() *userservice.Service {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.users
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
	a.users = nil
	a.simulator = nil
	a.webhookAudit = nil
	if db != nil {
		store := repository.NewPostgresStore(db)
		a.users, _ = userservice.New(store, userservice.Config{})
		a.simulator = simulatorruntime.NewService(store, nil)
		a.webhookAudit = webhookaudit.NewPersistentService(store)
	}
	return previousDB
}

func (a *app) buildInstallRuntime(result installResult, db *sql.DB) (runtimeSnapshot, error) {
	cfg := a.runtimeConfig()
	cfg.DatabaseURL = result.DatabaseURL
	cfg.RedisURL = result.RedisURL
	cfg.JWTSecret = result.JWTSecret
	cfg.WebhookSecretEncryptionKey = append([]byte(nil), result.WebhookSecretEncryptionKey...)
	cfg.Installed = true
	cloudProviders := newCloudProviderRegistry(cfg)
	store := repository.NewPostgresStore(db)
	projects, err := projectservice.New(store, projectservice.Config{
		EncryptionKeys: map[int][]byte{1: cfg.WebhookSecretEncryptionKey}, ActiveEncryptionKeyVersion: 1,
	})
	if err != nil {
		return runtimeSnapshot{}, fmt.Errorf("initialize Project service: %w", err)
	}
	devices, err := deviceservice.New(store, deviceServiceConfig(cfg))
	if err != nil {
		return runtimeSnapshot{}, fmt.Errorf("initialize Device service: %w", err)
	}
	commands, err := commandservice.New(store, commandServiceConfig(devices))
	if err != nil {
		return runtimeSnapshot{}, fmt.Errorf("initialize Command service: %w", err)
	}
	users, err := userservice.New(store, userservice.Config{})
	if err != nil {
		return runtimeSnapshot{}, fmt.Errorf("initialize User service: %w", err)
	}
	omniRuntime, err := newOmniRuntime(store, cfg)
	if err != nil {
		return runtimeSnapshot{}, fmt.Errorf("initialize Omni runtime: %w", err)
	}
	commandWorker, err := newPersistentCommandWorker(store, cloudProviders, omniRuntime.Adapter())
	if err != nil {
		_ = omniRuntime.Close()
		return runtimeSnapshot{}, fmt.Errorf("initialize Command worker: %w", err)
	}
	webhookWorker, err := newPersistentWebhookWorker(store, projects, cfg)
	if err != nil {
		_ = omniRuntime.Close()
		return runtimeSnapshot{}, fmt.Errorf("initialize Webhook worker: %w", err)
	}
	return runtimeSnapshot{
		cfg: cfg, db: db, auth: newDBAuthenticator(db, result.JWTSecret), projects: projects, devices: devices, commands: commands, users: users,
		simulator: simulatorruntime.NewService(store, nil), webhookAudit: webhookaudit.NewPersistentService(store),
		cloudProviders: cloudProviders, commandWorker: commandWorker, webhookWorker: webhookWorker,
		omniRuntime: omniRuntime,
	}, nil
}

func (a *app) publishRuntimeSnapshot(snapshot runtimeSnapshot) (*sql.DB, func()) {
	a.workerMu.Lock()
	stopWorker(a.commandCancel, a.commandDone)
	stopWorker(a.webhookCancel, a.webhookDone)
	stopOmniRuntime(a.omniCancel, a.omniDone, a.omniRuntime)

	a.runtimeMu.Lock()
	a.commandCancel, a.commandDone = nil, nil
	a.webhookCancel, a.webhookDone = nil, nil
	a.omniCancel, a.omniDone = nil, nil

	previousDB := a.db
	a.cfg = snapshot.cfg
	a.db = snapshot.db
	a.auth = snapshot.auth
	a.projects = snapshot.projects
	a.devices = snapshot.devices
	a.commands = snapshot.commands
	a.users = snapshot.users
	a.simulator = snapshot.simulator
	a.webhookAudit = snapshot.webhookAudit
	a.cloudProviders = snapshot.cloudProviders
	a.omniRuntime = snapshot.omniRuntime
	a.recoveryNeeded = true
	a.runtimeFailed = false
	a.runtimeMu.Unlock()
	a.workerMu.Unlock()

	activate := func() {
		a.workerMu.Lock()
		defer a.workerMu.Unlock()
		a.runtimeMu.Lock()
		defer a.runtimeMu.Unlock()
		a.omniCancel, a.omniDone = startOmniRuntime(a.logger, snapshot.omniRuntime, a.signalRuntimeFatal)
		a.commandCancel, a.commandDone = startCommandWorker(a.logger, snapshot.commandWorker)
		a.webhookCancel, a.webhookDone = startWebhookWorker(a.logger, snapshot.webhookWorker)
		a.recoveryNeeded = false
	}
	return previousDB, activate
}

func stopWorker(cancel context.CancelFunc, done <-chan struct{}) {
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func startCommandWorker(logger *slog.Logger, worker commandWorkerRunner) (context.CancelFunc, chan struct{}) {
	if worker == nil {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, func(err error) {
			logger.Error("persistent Command worker failed", slog.String("error", err.Error()))
		})
	}()
	return cancel, done
}

func startWebhookWorker(logger *slog.Logger, worker webhookWorkerRunner) (context.CancelFunc, chan struct{}) {
	if worker == nil {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, func(err error) {
			logger.Error("persistent Webhook worker failed", slog.String("error", err.Error()))
		})
	}()
	return cancel, done
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

func newPersistentCommandWorker(store commandworker.Store, providers cloudProviderRegistry, omniAdapter *omni.Adapter) (*commandworker.Worker, error) {
	client, ok := providers.WWTIOTClient(domain.ProviderCodeWWTIOT)
	if !ok {
		return nil, fmt.Errorf("wwtiot adapter is not registered")
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
		{
			ProviderCode: domain.ProviderCodeOmni, AdapterCode: domain.AdapterOmniDirectTCP,
			Adapter: omniAdapter, ResultSource: domain.EventSourceSystem,
		},
	}})
}

func newPersistentWebhookWorker(store webhookworker.Store, projects httpapi.ProjectService, cfg config) (*webhookworker.Worker, error) {
	resolver, ok := projects.(webhookworker.SecretResolver)
	if !ok {
		return nil, fmt.Errorf("project service cannot resolve webhook secret versions")
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

func newOmniRuntime(store repository.ProviderMessageStore, cfg config) (*omni.Runtime, error) {
	return omni.OpenRuntime(store, omni.RuntimeConfig{
		BikeAddress: cfg.OmniBikeListenAddr, IoTAddress: cfg.OmniIoTListenAddr,
		MaxFrameBytes: cfg.OmniMaxFrameBytes, MaxConnections: cfg.OmniMaxConnections,
		ReadTimeout: cfg.OmniReadTimeout,
	})
}

func startOmniRuntime(logger *slog.Logger, runtime omniRuntimeRunner, fatal func(error)) (context.CancelFunc, chan struct{}) {
	if runtime == nil || !runtime.Configured() {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := runtime.Run(ctx, func(err error) {
			logger.Error("Omni listener failed", slog.String("error", err.Error()))
		})
		if err != nil && ctx.Err() == nil && fatal != nil {
			fatal(err)
		}
	}()
	return cancel, done
}

func stopOmniRuntime(cancel context.CancelFunc, done <-chan struct{}, runtime omniRuntimeRunner) {
	if cancel != nil {
		cancel()
	}
	if runtime != nil {
		_ = runtime.Close()
	}
	if done != nil {
		<-done
	}
}

func (a *app) replaceOmniRuntime(runtime omniRuntimeRunner) {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	stopOmniRuntime(a.omniCancel, a.omniDone, a.omniRuntime)
	a.omniRuntime = runtime
	a.omniCancel, a.omniDone = startOmniRuntime(a.logger, runtime, a.signalRuntimeFatal)
}

func (a *app) close() error {
	a.replaceCommandWorker(nil)
	a.replaceWebhookWorker(nil)
	a.replaceOmniRuntime(nil)
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

func (a *app) deviceResourceService() httpapi.DeviceResourceService {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.devices
}

//lint:ignore U1000 used by integration-tagged HTTP tests
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

//lint:ignore U1000 used by integration-tagged HTTP tests
func (a *app) setCommandResourceService(service httpapi.CommandResourceService) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.commands = service
}

func (a *app) projectRequestMetadata(r *http.Request) projectservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return projectservice.RequestMetadata{
		ActorUserID: user.ID, IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func (a *app) deviceRequestMetadata(r *http.Request) deviceservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return deviceservice.RequestMetadata{
		ActorType: domain.ActorTypeUser, ActorUserID: user.ID,
		IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func (a *app) commandRequestMetadata(r *http.Request) commandservice.RequestMetadata {
	user, _ := userFromRequest(r)
	return commandservice.RequestMetadata{
		ActorType: domain.ActorTypeUser, ActorUserID: user.ID,
		IPAddress: clientIP(r), RequestID: httpjson.RequestID(r.Context()),
	}
}

func (a *app) humanScope(r *http.Request) access.Scope {
	user, _ := userFromRequest(r)
	if user.IsSuperAdmin {
		return access.SuperAdmin(user.ID)
	}
	return access.User(user.ID)
}

func deviceServiceConfig(cfg config) deviceservice.Config {
	allActions := map[domain.ActionIdentifier]domain.ProviderActionAvailability{
		domain.ActionIdentifier("unlock"):       domain.ProviderActionSupported,
		domain.ActionIdentifier("lock"):         domain.ProviderActionSupported,
		domain.ActionIdentifier("query_status"): domain.ProviderActionSupported,
	}
	wwtiotStatus := domain.ProviderIntegrationUnconfigured
	if (wwtiot.Config{APIURL: cfg.WWTIOTAPIURL, UserID: cfg.WWTIOTUserID, UserKey: cfg.WWTIOTUserKey}).Configured() {
		wwtiotStatus = domain.ProviderIntegrationConfiguredUnverified
	}
	omniStatus := domain.ProviderIntegrationUnconfigured
	if strings.TrimSpace(cfg.OmniBikeListenAddr) != "" && strings.TrimSpace(cfg.OmniIoTListenAddr) != "" {
		omniStatus = domain.ProviderIntegrationConfiguredUnverified
	}
	return deviceservice.Config{Providers: []deviceservice.ProviderRegistration{
		{
			Provider: deviceservice.Provider{
				Code: domain.ProviderCodeSimulator, Name: "Simulator", AccessType: domain.AccessTypeSimulator,
				TransportProtocol: domain.TransportProtocolInternal, Adapter: domain.AdapterSimulator,
				Profiles: []string{domain.ProviderProfileSimulatorV1},
				ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{
					domain.ProviderProfileSimulatorV1: allActions,
				},
				IntegrationStatus: domain.ProviderIntegrationVerified,
			},
			IdentityPolicy: deviceservice.DeviceIdentityPolicyFunc(simulatorruntime.NormalizeDeviceIdentity),
		},
		{
			Provider: deviceservice.Provider{
				Code: domain.ProviderCodeOmni, Name: "Omni", AccessType: domain.AccessTypeDirectDevice,
				TransportProtocol: domain.TransportProtocolTCP, Adapter: domain.AdapterOmniDirectTCP,
				Profiles: []string{domain.ProviderProfileOmniBikeV207, domain.ProviderProfileOmniIoTV135},
				ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{
					domain.ProviderProfileOmniBikeV207: {
						domain.ActionIdentifier("unlock"):       domain.ProviderActionMappingUnknown,
						domain.ActionIdentifier("lock"):         domain.ProviderActionUnsupported,
						domain.ActionIdentifier("query_status"): domain.ProviderActionSupported,
					},
					domain.ProviderProfileOmniIoTV135: {
						domain.ActionIdentifier("unlock"):       domain.ProviderActionMappingUnknown,
						domain.ActionIdentifier("lock"):         domain.ProviderActionMappingUnknown,
						domain.ActionIdentifier("query_status"): domain.ProviderActionSupported,
					},
				},
				IntegrationStatus: omniStatus,
			},
			IdentityPolicy: deviceservice.DeviceIdentityPolicyFunc(omni.NormalizeDeviceIdentity),
		},
		{
			Provider: deviceservice.Provider{
				Code: domain.ProviderCodeWWTIOT, Name: "WWTIOT", AccessType: domain.AccessTypeCloudAPI,
				TransportProtocol: domain.TransportProtocolHTTP, Adapter: domain.AdapterWWTIOTCloudAPI,
				Profiles: []string{domain.ProviderProfileWWTIOTV2},
				ProfileActions: map[string]map[domain.ActionIdentifier]domain.ProviderActionAvailability{
					domain.ProviderProfileWWTIOTV2: allActions,
				},
				IntegrationStatus: wwtiotStatus,
			},
			IdentityPolicy: deviceservice.DeviceIdentityPolicyFunc(wwtiot.NormalizeDeviceIdentity),
		},
	}}
}

func commandServiceConfig(devices *deviceservice.Service) commandservice.Config {
	providers := devices.ListProviders()
	registry := make([]domain.Provider, 0, len(providers))
	for _, provider := range providers {
		registry = append(registry, domain.Provider{
			Code: provider.Code, Name: provider.Name, AccessType: provider.AccessType,
			TransportProtocol: provider.TransportProtocol, Adapter: provider.Adapter,
			Profiles: provider.Profiles, ProfileActions: provider.ProfileActions,
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
	if err := storage.ValidateMigrationState(pingCtx, db); err != nil {
		return fmt.Errorf("database migration validation failed: %w", err)
	}
	if err := storage.ValidateFrozenContracts(pingCtx, db); err != nil {
		return fmt.Errorf("database contract validation failed: %w", err)
	}
	if err := testRedisConnection(ctx, redisURL); err != nil {
		return fmt.Errorf("redis unavailable after installation: %w", err)
	}
	return nil
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

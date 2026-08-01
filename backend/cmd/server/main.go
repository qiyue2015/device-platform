package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "device-platform failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 60*time.Second)
	recoveryErr := recoverInstallation(recoveryCtx)
	recoveryCancel()
	var cfg config
	var app *app
	var logger *slog.Logger
	if recoveryErr != nil {
		_ = loadEnvFiles(runtimeEnvPath())
		cfg = recoveryOnlyConfig()
		logger = newLogger(cfg.LogLevel)
		app = newAppWithServices(cfg, logger, nil, nil, nil, nil, nil, newCloudProviderRegistry(cfg), nil, nil)
		app.setRecoveryNeeded(true)
		logger.Error("installation recovery is required before runtime startup")
	} else {
		var err error
		cfg, err = loadConfig(runtimeEnvPath())
		if err != nil {
			return err
		}
		logger = newLogger(cfg.LogLevel)
		app, err = newApp(cfg, logger)
		if err != nil {
			return err
		}
	}
	defer app.close()
	logger.Info("device-platform listening", slog.String("addr", cfg.ServerAddr))

	server := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           app.routes(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-app.fatalErrors():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
			return errors.Join(err, fmt.Errorf("shutdown after fatal installation state: %w", shutdownErr))
		}
		serverErr := <-serverError
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			return errors.Join(err, serverErr)
		}
		return err
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown device-platform server: %w", err)
		}
		err := <-serverError
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func recoveryOnlyConfig() config {
	return config{
		ServerAddr:            envDefault("SERVER_ADDR", ":8080"),
		LogLevel:              envDefault("LOG_LEVEL", "info"),
		ReadHeaderTimeout:     envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		WebhookWorkerInterval: 2 * time.Second,
		WebhookRequestTimeout: 10 * time.Second,
		WebhookLeaseDuration:  15 * time.Second,
		WebhookMaxAttempts:    5,
		WebhookRetrySchedule:  []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute},
		WWTIOTAPIURL:          envDefault("WWTIOT_API_URL", "http://gps.wwtiot.com/api/"),
		WWTIOTUserID:          strings.TrimSpace(os.Getenv("WWTIOT_USER_ID")),
		WWTIOTUserKey:         os.Getenv("WWTIOT_USER_KEY"),
		OmniBikeListenAddr:    strings.TrimSpace(os.Getenv("OMNI_BIKE_LISTEN_ADDR")),
		OmniIoTListenAddr:     strings.TrimSpace(os.Getenv("OMNI_IOT_LISTEN_ADDR")),
		OmniMaxFrameBytes:     4096,
		OmniMaxConnections:    256,
		OmniReadTimeout:       5 * time.Minute,
	}
}

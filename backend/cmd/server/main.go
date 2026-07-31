package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	cfg, err := loadConfig(".env", "backend/.env")
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	app, err := newApp(cfg, logger)
	if err != nil {
		return err
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

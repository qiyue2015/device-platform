package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	app := newApp(cfg, logger)
	logger.Info("device-platform listening", slog.String("addr", cfg.ServerAddr))

	server := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           app.routes(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return server.ListenAndServe()
}

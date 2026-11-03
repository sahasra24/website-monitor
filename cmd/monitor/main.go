package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"website-monitor/internal/api"
	"website-monitor/internal/monitor"
	"website-monitor/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.databasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	checker := monitor.New(db, monitor.Config{
		Interval: cfg.interval,
		Timeout:  cfg.timeout,
		Workers:  cfg.workers,
		Retries:  cfg.retries,
	}, logger)

	server := &http.Server{
		Addr:              cfg.listenAddress,
		Handler:           api.New(db, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go checker.Run(ctx)
	go func() {
		logger.Info("HTTP server listening", "address", cfg.listenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown HTTP server", "error", err)
	}
}

type config struct {
	listenAddress string
	databasePath  string
	interval      time.Duration
	timeout       time.Duration
	workers       int
	retries       int
}

func loadConfig() (config, error) {
	cfg := config{
		listenAddress: envString("LISTEN_ADDR", ":8080"),
		databasePath:  envString("DATABASE_PATH", "monitor.db"),
		interval:      60 * time.Second,
		timeout:       10 * time.Second,
		workers:       10,
		retries:       1,
	}

	var err error
	if cfg.interval, err = envDuration("CHECK_INTERVAL", cfg.interval); err != nil {
		return config{}, err
	}
	if cfg.interval <= 0 {
		return config{}, errors.New("CHECK_INTERVAL must be greater than zero")
	}
	if cfg.timeout, err = envDuration("REQUEST_TIMEOUT", cfg.timeout); err != nil {
		return config{}, err
	}
	if cfg.timeout <= 0 {
		return config{}, errors.New("REQUEST_TIMEOUT must be greater than zero")
	}
	if cfg.workers, err = envInt("WORKERS", cfg.workers); err != nil {
		return config{}, err
	}
	if cfg.workers < 1 {
		return config{}, errors.New("WORKERS must be at least one")
	}
	if cfg.retries, err = envInt("RETRIES", cfg.retries); err != nil {
		return config{}, err
	}
	if cfg.retries < 0 {
		return config{}, errors.New("RETRIES cannot be negative")
	}
	return cfg, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, errors.New(key + ": " + err.Error())
	}
	return duration, nil
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New(key + ": " + err.Error())
	}
	return number, nil
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go-base-web-scraper/internal/api"
	"go-base-web-scraper/internal/config"
	"go-base-web-scraper/internal/db"
	"go-base-web-scraper/internal/queue"
	"go-base-web-scraper/internal/worker"
)

func main() {
	initLogger()

	cfg := config.FromEnv()
	slog.Info("Starting server", "port", cfg.Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("Failed to initialize database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := queue.Connect(cfg.RedisURL)
	if err != nil {
		slog.Error("Failed to connect to Redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	slog.Info("Connected to Redis and PostgreSQL")

	worker.Start(ctx, pool, rdb, cfg)

	handler := api.NewRouter(api.Handler{
		DB:     pool,
		Redis:  rdb,
		Config: cfg,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("API listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func initLogger() {
	level := slog.LevelInfo
	raw := os.Getenv("LOG_LEVEL")
	if raw == "" {
		raw = os.Getenv("RUST_LOG")
	}
	switch strings.ToLower(raw) {
	case "debug", "trace":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}

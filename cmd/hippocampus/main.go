package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/pkg/config"
)

//go:embed all:web_dist
var webDistEmbed embed.FS

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	migrationsDir := flag.String("migrations", "migrations", "path to SQL migrations directory")
	flag.Parse()

	if *showVersion {
		fmt.Printf("hippocampus %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := setupLogger(cfg.Log)
	logger.Info("starting hippocampus", "version", version, "rest_port", cfg.Server.RESTPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var spaFS fs.FS
	if sub, err := fs.Sub(webDistEmbed, "web_dist"); err == nil {
		spaFS = sub
		logger.Info("SPA embedded and will be served at /")
	}

	container, err := NewContainer(ctx, cfg, *migrationsDir, spaFS, logger)
	if err != nil {
		logger.Error("initialization failed", "error", err)
		os.Exit(1)
	}

	container.Start(ctx, cancel)

	logger.Info("hippocampus ready", "mcp", "stdio", "rest", container.httpSrv.Addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", "signal", sig.String())
		cancel()
	case <-ctx.Done():
		logger.Info("shutting down", "reason", "context cancelled")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	container.Shutdown(shutdownCtx)

	logger.Info("hippocampus stopped")
}

func setupLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

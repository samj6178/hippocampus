package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/hippocampus-mcp/hippocampus/internal/embedding"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/config"
	"github.com/hippocampus-mcp/hippocampus/internal/repo"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := repo.NewPool(ctx, repo.DBConfig{
		DSN:      cfg.Database.DSN(),
		MaxConns: 5,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	episodicRepo := repo.NewEpisodicRepo(pool)
	semanticRepo := repo.NewSemanticRepo(pool)
	proceduralRepo := repo.NewProceduralRepo(pool)
	projectRepo := repo.NewProjectRepo(pool)

	embProvider := embedding.NewOpenAIProvider(
		cfg.OpenAI.APIKey, cfg.OpenAI.Model, cfg.OpenAI.MaxBatch,
		cfg.Memory.EmbeddingCacheSize,
		embedding.WithBaseURL(cfg.OpenAI.BaseURL),
		embedding.WithDimensions(cfg.OpenAI.Dimensions),
		embedding.WithLogger(logger),
	)

	workingMem := memory.NewWorkingMemory(memory.WorkingMemoryConfig{
		Capacity:         cfg.Memory.WorkingMemoryCapacity,
		DecayHalfLifeSec: cfg.Memory.DecayHalfLifeDays * 86400,
	})

	emotionalRepo := repo.NewEmotionalTagRepo(pool)
	encodeSvc := app.NewEncodeService(
		episodicRepo, emotionalRepo, embProvider, workingMem,
		app.EncodeServiceConfig{GateThreshold: cfg.Memory.GateThreshold},
		logger,
	)

	recallSvc := app.NewRecallService(
		episodicRepo, semanticRepo, proceduralRepo,
		embProvider, workingMem,
		app.RecallServiceConfig{DecayHalfLifeDays: cfg.Memory.DecayHalfLifeDays},
		logger,
	)

	projectSvc := app.NewProjectService(projectRepo, logger)

	bench := app.NewBenchmarkSuite(encodeSvc, recallSvc, projectSvc, embProvider, logger)

	fmt.Fprintf(os.Stderr, "Starting benchmark...\n")
	start := time.Now()

	report, err := bench.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Benchmark completed in %s\n\n", time.Since(start).Round(time.Millisecond))
	fmt.Print(report.Formatted)
}

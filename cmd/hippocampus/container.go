package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	llmprovider "github.com/hippocampus-mcp/hippocampus/internal/adapter/llm"
	mcpserver "github.com/hippocampus-mcp/hippocampus/internal/adapter/mcp"
	restserver "github.com/hippocampus-mcp/hippocampus/internal/adapter/rest"
	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/hippocampus-mcp/hippocampus/internal/embedding"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/config"
	"github.com/hippocampus-mcp/hippocampus/internal/repo"
)

type Container struct {
	pool    *pgxpool.Pool
	httpSrv *http.Server
	logger  *slog.Logger

	consolidateSvc     *app.ConsolidateService
	contextWriter      *app.ContextWriter
	ruleGen            *app.RuleGenerator
	projectSvc         *app.ProjectService
	ingestSvc          *app.IngestService
	fileWatcher        *app.FileWatcher
	mcpSrv             *mcpserver.Server
	consolidateInt     time.Duration
	knowledgeScheduler *app.KnowledgeScheduler
	wg                 sync.WaitGroup
}

func NewContainer(ctx context.Context, cfg *config.Config, migrationsDir string, spaFS fs.FS, logger *slog.Logger) (*Container, error) {
	pool, err := repo.NewPool(ctx, repo.DBConfig{
		DSN:      cfg.Database.DSN(),
		MaxConns: int32(cfg.Database.MaxConns),
	})
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	logger.Info("database connected")

	if err := repo.RunMigrations(ctx, pool, migrationsDir, logger); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	logger.Info("migrations applied")

	// Repositories
	episodicRepo := repo.NewEpisodicRepo(pool)
	semanticRepo := repo.NewSemanticRepo(pool)
	proceduralRepo := repo.NewProceduralRepo(pool)
	projectRepo := repo.NewProjectRepo(pool)
	causalRepo := repo.NewCausalRepo(pool)
	emotionalRepo := repo.NewEmotionalTagRepo(pool)

	// Providers
	embProvider := embedding.NewOpenAIProvider(
		cfg.OpenAI.APIKey, cfg.OpenAI.Model, cfg.OpenAI.MaxBatch, cfg.Memory.EmbeddingCacheSize,
		embedding.WithBaseURL(cfg.OpenAI.BaseURL),
		embedding.WithDimensions(cfg.OpenAI.Dimensions),
		embedding.WithLogger(logger),
	)
	logger.Info("embedding provider ready", "model", embProvider.ModelID())

	var llmSwitch *llmprovider.SwitchableProvider
	if cfg.LLM.Provider == "none" || cfg.LLM.Provider == "" {
		llmSwitch = llmprovider.NewSwitchableProvider(nil, logger)
		logger.Info("LLM provider: none (agent-delegated mode)")
	} else {
		initialLLM := llmprovider.NewOpenAICompatProvider(llmprovider.ProviderConfig{
			BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey,
			Model: cfg.LLM.Model, MaxRPM: cfg.LLM.MaxRPM,
			MaxConcurrent: cfg.LLM.MaxConcurrent,
		}, logger)
		llmSwitch = llmprovider.NewSwitchableProvider(initialLLM, logger)
		logger.Info("LLM provider ready", "name", llmSwitch.Name())
	}

	// Working Memory
	workingMem := memory.NewWorkingMemory(memory.WorkingMemoryConfig{
		Capacity:         cfg.Memory.WorkingMemoryCapacity,
		DecayHalfLifeSec: cfg.Memory.DecayHalfLifeDays * 86400,
	})

	// Application Services
	encodeSvc := app.NewEncodeService(
		episodicRepo, emotionalRepo, embProvider, workingMem,
		app.EncodeServiceConfig{GateThreshold: cfg.Memory.GateThreshold}, logger,
	)
	recallSvc := app.NewRecallService(
		episodicRepo, semanticRepo, proceduralRepo, embProvider, workingMem,
		app.RecallServiceConfig{DecayHalfLifeDays: cfg.Memory.DecayHalfLifeDays}, logger, llmSwitch,
	)

	causalDetector := app.NewCausalDetector(causalRepo, episodicRepo, embProvider, logger)
	proceduralSvc := app.NewProceduralService(proceduralRepo, embProvider, logger)
	encodeSvc.SetCausalDetector(causalDetector)
	encodeSvc.SetProceduralService(proceduralSvc)
	encodeSvc.SetSemanticRepo(semanticRepo)
	recallSvc.SetCausalDetector(causalDetector)

	projectSvc := app.NewProjectService(projectRepo, logger)
	memorySvc := app.NewMemoryService(episodicRepo, semanticRepo, projectRepo, embProvider, workingMem, logger)
	ingestSvc := app.NewIngestService(semanticRepo, embProvider, logger)

	consolidateSvc := app.NewConsolidateService(
		episodicRepo, semanticRepo, embProvider, projectSvc,
		app.ConsolidateConfig{ClusterThreshold: 0.72, MinClusterSize: 2}, logger,
	)
	consolidateSvc.SetLLM(llmSwitch)

	healthSvc := app.NewHealthService(episodicRepo, semanticRepo, embProvider, logger)
	contextWriter := app.NewContextWriter(episodicRepo, semanticRepo, projectSvc, logger)
	ruleGen := app.NewRuleGenerator(episodicRepo, semanticRepo, embProvider, projectSvc, logger)
	metricsSvc := app.NewMetricsService(episodicRepo, semanticRepo, logger)
	predictionSvc := app.NewPredictionService(encodeSvc, embProvider, logger)
	ollamaBase := strings.TrimSuffix(cfg.OpenAI.BaseURL, "/v1")
	researchAgent := app.NewResearchAgent(encodeSvc, embProvider, ollamaBase, "qwen2.5:7b", logger, llmSwitch)
	evalFramework := app.NewEvalFramework(logger)
	benchmarkSuite := app.NewBenchmarkSuite(encodeSvc, recallSvc, projectSvc, embProvider, logger)
	studySvc := app.NewStudyService(encodeSvc, logger)
	analogizeSvc := app.NewAnalogizeService(episodicRepo, semanticRepo, embProvider, projectSvc, logger, llmSwitch)
	metaSvc := app.NewMetaService(episodicRepo, semanticRepo, proceduralRepo, predictionSvc, evalFramework, projectSvc, logger)

	// Hybrid Retriever + Cross-Encoder
	hybridRetriever := app.NewHybridRetriever(episodicRepo, semanticRepo, proceduralRepo, embProvider, logger)
	crossEncoder := app.NewCrossEncoder(llmSwitch, logger)
	recallSvc.SetHybridRetriever(hybridRetriever)

	// Knowledge Agents
	agentConfigs := app.DefaultAgentConfigs()
	knowledgeAgents := make([]*app.KnowledgeAgent, 0, len(agentConfigs))
	for _, ac := range agentConfigs {
		knowledgeAgents = append(knowledgeAgents, app.NewKnowledgeAgent(ac, llmSwitch, encodeSvc, embProvider, logger))
	}
	researchInterval, _ := time.ParseDuration(cfg.Research.SchedulerInterval)
	knowledgeScheduler := app.NewKnowledgeScheduler(knowledgeAgents, metaSvc, researchInterval, cfg.Research.MaxConcurrentAgents, logger)

	fusionEngine := app.NewFusionEngine(hybridRetriever, crossEncoder, causalDetector, proceduralSvc, llmSwitch, logger)
	warningMatcher := app.NewWarningMatcher(semanticRepo, embProvider, logger)
	preventionAnalyzer := app.NewPreventionAnalyzer(logger)
	abBenchmark := app.NewABBenchmark(logger)

	// MCP Server
	mcpSrv := mcpserver.NewServer(
		encodeSvc, recallSvc, projectSvc, consolidateSvc, memorySvc, ingestSvc,
		healthSvc, contextWriter, ruleGen, metricsSvc, predictionSvc, researchAgent,
		evalFramework, benchmarkSuite, studySvc, analogizeSvc, metaSvc, proceduralSvc,
		knowledgeScheduler, fusionEngine, warningMatcher, preventionAnalyzer, abBenchmark, episodicRepo, llmSwitch, logger,
	)

	// REST Server
	restSrv := restserver.NewServer(
		encodeSvc, recallSvc, projectSvc, memorySvc, consolidateSvc,
		proceduralSvc, contextWriter, benchmarkSuite, llmSwitch, healthSvc, logger, spaFS,
	)
	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.RESTPort),
		Handler:      restSrv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	return &Container{
		pool:               pool,
		httpSrv:            httpSrv,
		logger:             logger,
		consolidateSvc:     consolidateSvc,
		contextWriter:      contextWriter,
		ruleGen:            ruleGen,
		projectSvc:         projectSvc,
		ingestSvc:          ingestSvc,
		fileWatcher:        app.NewFileWatcher(ingestSvc, logger),
		mcpSrv:             mcpSrv,
		consolidateInt:     cfg.Memory.ConsolidationInterval,
		knowledgeScheduler: knowledgeScheduler,
	}, nil
}

func (c *Container) Start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		if err := c.mcpSrv.Run(ctx); err != nil {
			c.logger.Error("MCP server error", "error", err)
			cancel()
			return
		}
		c.logger.Info("MCP server stdin closed (REST server continues running)")
	}()

	go func() {
		c.logger.Info("REST server starting", "addr", c.httpSrv.Addr)
		if err := c.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("REST server error", "error", err)
		}
	}()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.startFileWatcher(ctx)
	}()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.startupConsolidation(ctx)
	}()

	if c.consolidateInt > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.consolidationTimer(ctx)
		}()
	}
}

func (c *Container) Shutdown(ctx context.Context) {
	// 1. Stop accepting new scheduled work
	if c.knowledgeScheduler != nil {
		c.knowledgeScheduler.Stop()
	}

	// 2. Stop file watcher
	if c.fileWatcher != nil {
		c.fileWatcher.Stop()
	}

	// 3. Wait for pending background goroutines (consolidation, context writes)
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		c.logger.Info("all background tasks completed")
	case <-ctx.Done():
		c.logger.Warn("shutdown timeout, some background tasks may not have completed")
	}

	// 4. HTTP server graceful shutdown
	c.httpSrv.Shutdown(ctx)

	// 5. Close DB pool last (everything else may need it)
	c.pool.Close()
}

func (c *Container) startFileWatcher(ctx context.Context) {
	time.Sleep(3 * time.Second)
	projects, err := c.projectSvc.List(ctx)
	if err != nil {
		c.logger.Warn("failed to list projects for file watcher", "error", err)
		return
	}
	for _, p := range projects {
		if p.RootPath != "" {
			if err := c.fileWatcher.WatchProject(p.RootPath, &p.ID); err != nil {
				c.logger.Warn("failed to watch project", "slug", p.Slug, "error", err)
			}
		}
	}
	if err := c.fileWatcher.Start(ctx); err != nil {
		c.logger.Warn("file watcher failed to start", "error", err)
	}
}

func (c *Container) startupConsolidation(ctx context.Context) {
	time.Sleep(5 * time.Second)
	c.logger.Info("running startup consolidation + decay + context write")
	if results, err := c.consolidateSvc.RunAll(ctx); err != nil {
		c.logger.Warn("startup consolidation failed", "error", err)
	} else {
		for _, r := range results {
			c.logger.Info("startup consolidation result",
				"project_id", r.ProjectID,
				"semantic_created", r.SemanticCreated,
				"episodes_marked", r.EpisodesMarked,
			)
		}
	}
	c.contextWriter.WriteAll(ctx)
	c.ruleGen.GenerateAll(ctx)
}

func (c *Container) consolidationTimer(ctx context.Context) {
	ticker := time.NewTicker(c.consolidateInt)
	defer ticker.Stop()
	c.logger.Info("consolidation timer started", "interval", c.consolidateInt)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if results, err := c.consolidateSvc.RunAll(ctx); err != nil {
				c.logger.Error("scheduled consolidation failed", "error", err)
			} else {
				for _, r := range results {
					c.logger.Info("scheduled consolidation result",
						"project_id", r.ProjectID,
						"semantic_created", r.SemanticCreated,
						"episodes_marked", r.EpisodesMarked,
					)
				}
			}
			c.contextWriter.WriteAll(ctx)
			c.ruleGen.GenerateAll(ctx)
		}
	}
}

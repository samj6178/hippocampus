package rest

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hippocampus-mcp/hippocampus/internal/adapter/llm"
	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	encode        *app.EncodeService
	recall        *app.RecallService
	project       *app.ProjectService
	memory        *app.MemoryService
	consolidate   *app.ConsolidateService
	proceduralSvc *app.ProceduralService
	ctxWriter     *app.ContextWriter
	benchmark     *app.BenchmarkSuite
	llmSwitch     *llm.SwitchableProvider
	health        *app.HealthService
	logger        *slog.Logger
	router        chi.Router
	spaFS         fs.FS
}

func NewServer(
	encode *app.EncodeService,
	recall *app.RecallService,
	project *app.ProjectService,
	memory *app.MemoryService,
	consolidate *app.ConsolidateService,
	proceduralSvc *app.ProceduralService,
	ctxWriter *app.ContextWriter,
	benchmark *app.BenchmarkSuite,
	llmSwitch *llm.SwitchableProvider,
	health *app.HealthService,
	logger *slog.Logger,
	spaFS fs.FS,
) *Server {
	s := &Server{
		encode:        encode,
		recall:        recall,
		project:       project,
		memory:        memory,
		consolidate:   consolidate,
		proceduralSvc: proceduralSvc,
		ctxWriter:     ctxWriter,
		benchmark:     benchmark,
		llmSwitch:     llmSwitch,
		health:        health,
		logger:        logger,
		spaFS:         spaFS,
	}
	s.router = s.buildRouter()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealthReady)
		r.Get("/health/live", s.handleHealthLive)
		r.Get("/health/ready", s.handleHealthReady)
		r.Get("/stats", s.handleStats)

		r.Route("/memories", func(r chi.Router) {
			r.Get("/", s.handleListMemories)
			r.Post("/", s.handleCreateMemory)
			r.Post("/recall", s.handleRecall)
			r.Post("/feedback", s.handleFeedback)
			r.Get("/{id}", s.handleGetMemory)
			r.Delete("/{id}", s.handleDeleteMemory)
		})

		r.Post("/consolidate", s.handleConsolidate)
		r.Post("/benchmark", s.handleBenchmark)

		r.Route("/projects", func(r chi.Router) {
			r.Get("/", s.handleListProjects)
			r.Post("/", s.handleCreateProject)
			r.Get("/{slug}", s.handleGetProject)
			r.Get("/{slug}/stats", s.handleGetProjectStats)
			r.Delete("/{slug}", s.handleDeleteProject)
		})

		r.Route("/settings", func(r chi.Router) {
			r.Get("/llm", s.handleGetLLMSettings)
			r.Put("/llm", s.handleUpdateLLMSettings)
			r.Post("/llm/test", s.handleTestLLMConnection)
		})
	})

	r.Handle("/metrics", promhttp.Handler())

	if s.spaFS != nil {
		r.NotFound(ServeSPA(s.spaFS))
	}

	return r
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "hippocampus-mos"
	serverVersion   = "0.1.0"
)

// Server implements the MCP (Model Context Protocol) JSON-RPC 2.0 server
// over stdio with newline-delimited JSON (MCP stdio transport spec 2025-11-25).
// This is the primary interface for Cursor and Claude Code.
type Server struct {
	encode      *app.EncodeService
	recall      *app.RecallService
	project     *app.ProjectService
	consolidate *app.ConsolidateService
	memory      *app.MemoryService
	ingest      *app.IngestService
	health      *app.HealthService
	ctxWriter   *app.ContextWriter
	ruleGen     *app.RuleGenerator
	metrics     *app.MetricsService
	prediction  *app.PredictionService
	research    *app.ResearchAgent
	eval        *app.EvalFramework
	benchmark   *app.BenchmarkSuite
	study       *app.StudyService
	analogize   *app.AnalogizeService
	meta        *app.MetaService
	proceduralSvc *app.ProceduralService
	scheduler   *app.KnowledgeScheduler
	fusion             *app.FusionEngine
	warningMatcher     *app.WarningMatcher
	preventionAnalyzer *app.PreventionAnalyzer
	abBenchmark        *app.ABBenchmark
	episodic           domain.EpisodicRepo
	logger             *slog.Logger

	pendingOps sync.WaitGroup

	mu     sync.Mutex
	writer io.Writer

	sessionMu          sync.Mutex
	sessionCalls       int
	sessionRecalls     int
	sessionRemember    int
	sessionFeedback    int
	clientName         string
	sessionStartCommit string

	exposedMu       sync.Mutex
	exposedWarnings []app.MatchedWarning

	preventionMu        sync.Mutex
	totalPrevented      int
	totalIgnored        int
	totalExposed        int
	totalNotApplicable  int
}

func NewServer(
	encode *app.EncodeService,
	recall *app.RecallService,
	project *app.ProjectService,
	consolidate *app.ConsolidateService,
	memory *app.MemoryService,
	ingest *app.IngestService,
	health *app.HealthService,
	ctxWriter *app.ContextWriter,
	ruleGen *app.RuleGenerator,
	metrics *app.MetricsService,
	prediction *app.PredictionService,
	research *app.ResearchAgent,
	eval *app.EvalFramework,
	benchmark *app.BenchmarkSuite,
	study *app.StudyService,
	analogize *app.AnalogizeService,
	meta *app.MetaService,
	proceduralSvc *app.ProceduralService,
	scheduler *app.KnowledgeScheduler,
	fusion *app.FusionEngine,
	warningMatcher *app.WarningMatcher,
	preventionAnalyzer *app.PreventionAnalyzer,
	abBenchmark *app.ABBenchmark,
	episodic domain.EpisodicRepo,
	logger *slog.Logger,
) *Server {
	return &Server{
		encode:             encode,
		recall:             recall,
		project:            project,
		consolidate:        consolidate,
		memory:             memory,
		ingest:             ingest,
		health:             health,
		ctxWriter:          ctxWriter,
		ruleGen:            ruleGen,
		metrics:            metrics,
		prediction:         prediction,
		research:           research,
		eval:               eval,
		benchmark:          benchmark,
		study:              study,
		analogize:          analogize,
		meta:               meta,
		proceduralSvc:      proceduralSvc,
		scheduler:          scheduler,
		fusion:             fusion,
		warningMatcher:     warningMatcher,
		preventionAnalyzer: preventionAnalyzer,
		abBenchmark:        abBenchmark,
		episodic:           episodic,
		logger:             logger,
		writer:             os.Stdout,
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Run starts the MCP server reading newline-delimited JSON-RPC messages
// from stdin and writing responses to stdout (one JSON object per line).
// This follows the MCP stdio transport specification (2025-11-25).
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info("MCP server starting on stdio (newline-delimited JSON)")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				s.logger.Error("stdin read error", "error", err)
				return err
			}
			s.logger.Info("stdin closed, shutting down")
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "parse error: "+err.Error())
			continue
		}

		s.handleRequest(ctx, &req)
	}
}

func (s *Server) handleRequest(ctx context.Context, req *jsonRPCRequest) {
	s.logger.Debug("handling request", "method", req.Method, "id", req.ID)

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		// notification, no response needed
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolCall(ctx, req)
	case "resources/list":
		s.handleResourcesList(req)
	case "resources/read":
		s.handleResourcesRead(ctx, req)
	case "ping":
		s.sendResult(req.ID, map[string]string{})
	default:
		s.sendError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req *jsonRPCRequest) {
	// Auto-detect client environment from initialize params
	if req.Params != nil {
		var initParams struct {
			ClientInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
			Roots []struct {
				URI  string `json:"uri"`
				Name string `json:"name"`
			} `json:"roots"`
		}
		if err := json.Unmarshal(req.Params, &initParams); err == nil {
			if initParams.ClientInfo.Name != "" {
				s.clientName = initParams.ClientInfo.Name
				env := app.DetectEnvFromClientName(initParams.ClientInfo.Name)
				s.logger.Info("detected client environment",
					"client", initParams.ClientInfo.Name,
					"env", string(env),
				)
			}
			for _, root := range initParams.Roots {
				rootPath := root.URI
				if strings.HasPrefix(rootPath, "file://") {
					rootPath = strings.TrimPrefix(rootPath, "file://")
					rootPath = strings.TrimPrefix(rootPath, "/")
				}
				if rootPath != "" {
					identity := app.DetectProject(rootPath)
					if identity != nil && identity.Slug != "" {
						project, err := s.project.FindOrCreate(context.Background(), identity)
						if err == nil && project != nil {
							setActiveProject(&project.ID, project.Slug)
							s.logger.Info("auto-detected project from roots",
								"slug", project.Slug,
								"id", project.ID,
								"root", rootPath,
							)
						}
					}
				}
			}
		}
	}

	s.sendResult(req.ID, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
	})
}

func (s *Server) handleToolsList(req *jsonRPCRequest) {
	s.sendResult(req.ID, map[string]any{
		"tools": tools(),
	})
}

func (s *Server) trackToolCall(name string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionCalls++
	metrics.MCPToolCalls.WithLabelValues(name).Inc()
	switch name {
	case "mos_recall", "mos_file_context":
		s.sessionRecalls++
	case "mos_remember", "mos_learn_error", "mos_session_end":
		s.sessionRemember++
	case "mos_feedback", "mos_track_outcome":
		s.sessionFeedback++
	}
}

func (s *Server) agentID() string {
	if s.clientName != "" {
		return s.clientName
	}
	return "unknown"
}

func (s *Server) learningLoopHint() string {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if s.sessionRecalls >= 3 && s.sessionRemember == 0 {
		return " [HINT: You've done " + fmt.Sprintf("%d", s.sessionRecalls) + " recalls but 0 remember/learn calls. Use mos_remember to save important findings.]"
	}
	if s.sessionRecalls >= 3 && s.sessionFeedback == 0 {
		return " [HINT: " + fmt.Sprintf("%d", s.sessionRecalls) + " recalls with 0 feedback. Use mos_feedback to rate recall quality so MOS can learn.]"
	}
	if s.sessionCalls >= 10 && s.sessionRemember == 0 && s.sessionRecalls == 0 {
		return " [HINT: " + fmt.Sprintf("%d", s.sessionCalls) + " tool calls but no recall/remember. Consider using mos_recall for context and mos_remember to save decisions.]"
	}
	return ""
}

func (s *Server) handleToolCall(ctx context.Context, req *jsonRPCRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	const maxArgumentsSize = 256 * 1024 // 256 KB
	if len(params.Arguments) > maxArgumentsSize {
		s.sendError(req.ID, -32602, fmt.Sprintf("arguments too large: %d bytes (max %d)", len(params.Arguments), maxArgumentsSize))
		return
	}

	s.trackToolCall(params.Name)

	var result any
	var err error

	switch params.Name {
	case "mos_init":
		result, err = s.toolInit(ctx, params.Arguments)
	case "mos_learn_error":
		result, err = s.toolLearnError(ctx, params.Arguments)
	case "mos_remember":
		result, err = s.toolRemember(ctx, params.Arguments)
	case "mos_recall":
		result, err = s.toolRecall(ctx, params.Arguments)
	case "mos_switch_project":
		result, err = s.toolSwitchProject(ctx, params.Arguments)
	case "mos_list_projects":
		result, err = s.toolListProjects(ctx)
	case "mos_create_project":
		result, err = s.toolCreateProject(ctx, params.Arguments)
	case "mos_consolidate":
		result, err = s.toolConsolidate(ctx, params.Arguments)
	case "mos_feedback":
		result, err = s.toolFeedback(ctx, params.Arguments)
	case "mos_ingest_codebase":
		result, err = s.toolIngestCodebase(ctx, params.Arguments)
	case "mos_session_end":
		result, err = s.toolSessionEnd(ctx, params.Arguments)
	case "mos_health":
		result = s.health.Check(ctx)
	case "mos_predict":
		result, err = s.toolPredict(ctx, params.Arguments)
	case "mos_resolve":
		result, err = s.toolResolve(ctx, params.Arguments)
	case "mos_research":
		result, err = s.toolResearch(ctx, params.Arguments)
	case "mos_benchmark":
		result, err = s.toolBenchmark(ctx)
	case "mos_study_project":
		result, err = s.toolStudyProject(ctx)
	case "mos_analogize":
		result, err = s.toolAnalogize(ctx, params.Arguments)
	case "mos_meta":
		result, err = s.toolMeta(ctx)
	case "mos_track_outcome":
		result, err = s.toolTrackOutcome(ctx, params.Arguments)
	case "mos_evaluate":
		result, err = s.toolEvaluate(ctx, params.Arguments)
	case "mos_file_context":
		result, err = s.toolFileContext(ctx, params.Arguments)
	case "mos_metrics":
		result, err = s.toolMetrics(ctx)
	case "mos_curate":
		result, err = s.toolCurate(ctx, params.Arguments)
	case "mos_fuse":
		result, err = s.toolFuse(ctx, params.Arguments)
	case "mos_cite":
		result, err = s.toolCite(ctx, params.Arguments)
	case "mos_ab_test":
		result, err = s.toolABTest(ctx)
	default:
		s.sendError(req.ID, -32602, "unknown tool: "+params.Name)
		return
	}

	if err != nil {
		s.logger.Error("tool execution failed", "tool", params.Name, "error", err)

		// Observation layer: auto-capture tool errors as episodic memory.
		// Skip self-referential tools to avoid infinite loops.
		if params.Name != "mos_remember" && params.Name != "mos_learn_error" &&
			params.Name != "mos_recall" && params.Name != "mos_init" {
			go s.autoCapture(context.Background(), params.Name, err.Error())
		}

		s.sendResult(req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
			},
			"isError": true,
		})
		return
	}

	text, _ := json.Marshal(result)
	hint := s.learningLoopHint()
	s.sendResult(req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(text) + hint},
		},
	})

	// Passive error observation: detect error reports in successful tool calls
	go s.observeResult(context.Background(), params.Name, params.Arguments)
}

// autoCapture silently stores tool errors as episodic memories.
// This is the observation layer: the system learns from errors without
// the agent explicitly calling mos_learn_error.
func (s *Server) autoCapture(ctx context.Context, toolName, errMsg string) {
	projectID := getActiveProject()

	content := fmt.Sprintf("AUTO-CAPTURED ERROR in tool %s: %s", toolName, errMsg)
	if len(content) > 500 {
		content = content[:500]
	}

	tags := []string{"auto_captured", "error", "tool:" + toolName}
	for _, fp := range extractFilePaths(errMsg) {
		tags = append(tags, "file:"+fp)
	}

	_, err := s.encode.Encode(ctx, &app.EncodeRequest{
		Content:    content,
		ProjectID:  projectID,
		AgentID:    s.agentID(),
		SessionID:  uuid.New(),
		Importance: 0.7,
		Tags:       tags,
	})
	if err != nil {
		s.logger.Warn("auto-capture failed", "error", err)
	} else {
		metrics.AutoCapturedErrors.Inc()
	}
}

// observeResult passively detects error reports in successful tool call arguments.
// When an agent calls mos_remember or mos_session_end with content that looks like
// an error report, it's auto-captured without explicit mos_learn_error.
func (s *Server) observeResult(ctx context.Context, toolName string, args json.RawMessage) {
	if toolName != "mos_remember" && toolName != "mos_session_end" {
		return
	}

	var parsed struct {
		Content string `json:"content"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return
	}

	text := parsed.Content + " " + parsed.Summary
	if !looksLikeError(text) {
		return
	}

	projectID := getActiveProject()
	tags := []string{"auto_captured", "error", "observed", "tool:" + toolName}
	for _, fp := range extractFilePaths(text) {
		tags = append(tags, "file:"+fp)
	}

	content := text
	if len(content) > 500 {
		content = content[:500]
	}

	_, err := s.encode.Encode(ctx, &app.EncodeRequest{
		Content:    content,
		ProjectID:  projectID,
		AgentID:    s.agentID(),
		SessionID:  uuid.New(),
		Importance: 0.75,
		Tags:       tags,
	})
	if err != nil {
		s.logger.Debug("observe-result capture failed", "error", err)
	} else {
		metrics.AutoCapturedErrors.Inc()
	}
}

var filePathRe = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[/\\][\w._-]+)+\.(?:go|ts|tsx|py|rs|js|jsx|java|sql|yaml|yml|json|toml|rb|cs|cpp|c|h)`)

func extractFilePaths(text string) []string {
	matches := filePathRe.FindAllString(text, 5)
	seen := map[string]bool{}
	var result []string
	for _, m := range matches {
		norm := strings.ReplaceAll(m, "\\", "/")
		if !seen[norm] {
			result = append(result, norm)
			seen[norm] = true
		}
	}
	return result
}

func looksLikeError(text string) bool {
	lower := strings.ToLower(text)
	indicators := []string{
		"error:", "bug:", "panic:", "fatal:", "failed:",
		"root cause:", "sqlstate", "traceback", "exception:",
		"compilation failed", "build failed", "test failed",
		"nil pointer", "index out of range", "segmentation fault",
		"connection refused", "timeout exceeded",
	}
	count := 0
	for _, ind := range indicators {
		if strings.Contains(lower, ind) {
			count++
		}
	}
	return count >= 2
}

func (s *Server) handleResourcesList(req *jsonRPCRequest) {
	s.sendResult(req.ID, map[string]any{
		"resources": []map[string]any{
			{
				"uri":         "mos://context",
				"name":        "Active Project Context",
				"description": "Auto-assembled context from all memory tiers for the active project. Load this at session start to restore memory continuity.",
				"mimeType":    "text/plain",
			},
			{
				"uri":         "mos://status",
				"name":        "Memory System Status",
				"description": "Current state: active project, memory counts, last consolidation, system health.",
				"mimeType":    "application/json",
			},
		},
	})
}

func (s *Server) handleResourcesRead(ctx context.Context, req *jsonRPCRequest) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	switch params.URI {
	case "mos://context":
		s.resourceContext(ctx, req)
	case "mos://status":
		s.resourceStatus(ctx, req)
	default:
		s.sendError(req.ID, -32602, "unknown resource: "+params.URI)
	}
}

func (s *Server) resourceContext(ctx context.Context, req *jsonRPCRequest) {
	projectID := getActiveProject()

	resp, err := s.recall.Recall(ctx, &app.RecallRequest{
		Query:     "project overview architecture current state recent changes decisions",
		ProjectID: projectID,
		Budget:    domain.DefaultBudget(),
	})

	var text string
	if err != nil || resp == nil {
		text = "No context available. Use mos_remember to store memories."
	} else {
		text = resp.Context.Text
	}

	s.sendResult(req.ID, map[string]any{
		"contents": []map[string]any{
			{
				"uri":      "mos://context",
				"mimeType": "text/plain",
				"text":     text,
			},
		},
	})
}

func (s *Server) resourceStatus(ctx context.Context, req *jsonRPCRequest) {
	stats, err := s.memory.Stats(ctx)

	status := map[string]any{
		"active_project": getActiveProjectSlug(),
		"version":        serverVersion,
	}

	if err == nil {
		status["episodic_count"] = stats.TotalEpisodic
		status["semantic_count"] = stats.TotalSemantic
		status["working_memory_fill"] = stats.WorkingMemFill
		status["embedding_model"] = stats.EmbeddingModel
	}

	statusJSON, _ := json.MarshalIndent(status, "", "  ")

	s.sendResult(req.ID, map[string]any{
		"contents": []map[string]any{
			{
				"uri":      "mos://status",
				"mimeType": "application/json",
				"text":     string(statusJSON),
			},
		},
	})
}

func (s *Server) sendResult(id any, result any) {
	s.send(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(id any, code int, message string) {
	s.send(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

// send writes a JSON-RPC response as a single line of JSON followed by newline.
// Per MCP stdio transport spec, messages MUST NOT contain embedded newlines.
func (s *Server) send(resp jsonRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Error("failed to marshal response", "error", err)
		return
	}

	data = append(data, '\n')
	if _, err := s.writer.Write(data); err != nil {
		s.logger.Error("failed to write response", "error", err)
	}
}

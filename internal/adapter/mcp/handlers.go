package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
)

var (
	activeProject     *uuid.UUID
	activeProjectSlug string
	activeProjectMu   sync.RWMutex
)

func getActiveProject() *uuid.UUID {
	activeProjectMu.RLock()
	defer activeProjectMu.RUnlock()
	if activeProject == nil {
		return nil
	}
	cp := *activeProject
	return &cp
}

func getActiveProjectSlug() string {
	activeProjectMu.RLock()
	defer activeProjectMu.RUnlock()
	return activeProjectSlug
}

func setActiveProject(id *uuid.UUID, slug string) {
	activeProjectMu.Lock()
	defer activeProjectMu.Unlock()
	activeProject = id
	activeProjectSlug = slug
}

// --- mos_init ---

type initArgs struct {
	WorkspacePath string `json:"workspace_path"`
	ProjectName   string `json:"project_name,omitempty"`
}

func (s *Server) toolInit(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args initArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.WorkspacePath == "" {
		return nil, fmt.Errorf("workspace_path is required")
	}

	wsPath := filepath.Clean(args.WorkspacePath)

	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	var matched *domain.Project
	for _, p := range projects {
		if p.RootPath == "" {
			continue
		}
		pPath := filepath.Clean(p.RootPath)
		if strings.EqualFold(pPath, wsPath) {
			matched = p
			break
		}
	}

	isNew := false
	if matched == nil {
		slug := filepath.Base(wsPath)
		slug = strings.ToLower(strings.ReplaceAll(slug, " ", "-"))

		displayName := args.ProjectName
		if displayName == "" {
			displayName = filepath.Base(wsPath)
		}

		created, err := s.project.Create(ctx, slug, displayName, "", wsPath)
		if err != nil {
			if strings.Contains(err.Error(), "slug") {
				slug = slug + "-" + uuid.New().String()[:4]
				created, err = s.project.Create(ctx, slug, displayName, "", wsPath)
				if err != nil {
					return nil, fmt.Errorf("create project: %w", err)
				}
			} else {
				return nil, fmt.Errorf("create project: %w", err)
			}
		}
		matched = created
		isNew = true
	}

	setActiveProject(&matched.ID, matched.Slug)

	result := map[string]any{
		"status":       "initialized",
		"project":      matched.Slug,
		"project_name": matched.DisplayName,
		"project_id":   matched.ID,
		"is_new":       isNew,
		"root_path":    matched.RootPath,
	}

	if isNew {
		ingestResult, ingestErr := s.ingest.IngestGoProject(ctx, wsPath, &matched.ID)
		if ingestErr != nil {
			result["ingest_error"] = ingestErr.Error()
		} else if ingestResult != nil {
			result["ingest"] = map[string]any{
				"files_scanned":    ingestResult.FilesScanned,
				"memories_created": ingestResult.MemoriesCreated,
				"entities_found":   ingestResult.EntitiesFound,
			}
		}
	}

	s.ctxWriter.WriteAll(ctx)

	if s.warningMatcher != nil {
		s.warningMatcher.LoadRules(ctx, &matched.ID)
		result["warning_rules_loaded"] = s.warningMatcher.RuleCount()
	}

	// Capture git HEAD at session start for prevention analysis scope
	if matched.RootPath != "" {
		s.sessionStartCommit = captureHeadCommit(ctx, matched.RootPath)
	}

	epCount, _ := s.memory.Stats(ctx)
	if epCount != nil {
		result["episodic_count"] = epCount.TotalEpisodic
		result["semantic_count"] = epCount.TotalSemantic
	}

	// Rich auto-context: inject session history, decisions, warnings
	// so the agent starts with full awareness without calling mos_recall.
	autoCtx := s.buildAutoContext(ctx, &matched.ID)
	if autoCtx != "" {
		result["auto_context"] = autoCtx
	}

	if isNew {
		result["message"] = fmt.Sprintf("New project '%s' created and codebase ingested. Context file generated at .cursor/rules/mos_context.mdc", matched.DisplayName)
	} else {
		result["message"] = fmt.Sprintf("Project '%s' activated. Use auto_context above for session continuity.", matched.DisplayName)
	}

	return result, nil
}

// --- mos_learn_error ---

type learnErrorArgs struct {
	ErrorMessage string `json:"error_message"`
	Context      string `json:"context,omitempty"`
	RootCause    string `json:"root_cause"`
	Fix          string `json:"fix"`
	Prevention   string `json:"prevention,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
	Project      string `json:"project,omitempty"`
}

func (s *Server) toolLearnError(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args learnErrorArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ErrorMessage == "" || args.RootCause == "" || args.Fix == "" {
		return nil, fmt.Errorf("error_message, root_cause, and fix are required")
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("ERROR: %s\n", args.ErrorMessage))
	if args.Context != "" {
		content.WriteString(fmt.Sprintf("CONTEXT: %s\n", args.Context))
	}
	content.WriteString(fmt.Sprintf("ROOT CAUSE: %s\n", args.RootCause))
	content.WriteString(fmt.Sprintf("FIX: %s\n", args.Fix))
	if args.Prevention != "" {
		content.WriteString(fmt.Sprintf("PREVENTION: %s\n", args.Prevention))
	}
	if args.FilePath != "" {
		content.WriteString(fmt.Sprintf("FILE: %s\n", args.FilePath))
	}

	tags := []string{"error", "bugfix", "learned_pattern"}
	if args.FilePath != "" {
		tags = append(tags, "file:"+args.FilePath)
	}

	resp, err := s.encode.Encode(ctx, &app.EncodeRequest{
		Content:    content.String(),
		ProjectID:  projectID,
		AgentID:    "error-learner",
		Importance: 0.9,
		Tags:       tags,
	})
	if err != nil {
		return nil, fmt.Errorf("store error pattern: %w", err)
	}

	s.metrics.RecordError()

	go func() {
		bgCtx := context.Background()
		// Note: StoreIfProcedural is already called inside EncodeService.Encode()
		s.ctxWriter.WriteAll(bgCtx)
		s.ruleGen.GenerateAll(bgCtx)
	}()

	return map[string]any{
		"status":    "learned",
		"memory_id": resp.MemoryID,
		"message":   "Error pattern stored. Fix auto-stored as procedure. Will appear in DO NOT REPEAT section.",
	}, nil
}

// --- mos_remember ---

type rememberArgs struct {
	Content    string   `json:"content"`
	Project    string   `json:"project,omitempty"`
	Importance float64  `json:"importance,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type rememberResult struct {
	Status     string    `json:"status"`
	MemoryID   uuid.UUID `json:"memory_id"`
	GateScore  float64   `json:"gate_score"`
	TokenCount int       `json:"token_count"`
}

func (s *Server) toolRemember(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args rememberArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	if args.Importance <= 0 {
		args.Importance = 0.5
	}

	resp, err := s.encode.Encode(ctx, &app.EncodeRequest{
		Content:    args.Content,
		ProjectID:  projectID,
		AgentID:    s.agentID(),
		SessionID:  uuid.New(),
		Importance: args.Importance,
		Tags:       args.Tags,
	})
	if err != nil {
		return nil, err
	}

	if !resp.Encoded {
		return map[string]any{
			"status":     "gated",
			"gate_score": resp.GateScore,
			"message":    "Memory did not pass importance gate. Increase importance or lower gate threshold.",
		}, nil
	}

	go func() {
		bgCtx := context.Background()
		// Note: StoreIfProcedural is already called inside EncodeService.Encode()
		s.ctxWriter.WriteAll(bgCtx)
	}()

	return rememberResult{
		Status:     "stored",
		MemoryID:   resp.MemoryID,
		GateScore:  resp.GateScore,
		TokenCount: resp.TokenCount,
	}, nil
}

// --- mos_recall ---

type recallArgs struct {
	Query       string `json:"query"`
	Project     string `json:"project,omitempty"`
	BudgetTokens int   `json:"budget_tokens,omitempty"`
}

type recallResult struct {
	Context    string `json:"context"`
	TokenCount int    `json:"token_count"`
	Sources    int    `json:"sources_used"`
	Candidates int    `json:"candidates_considered"`
	Confidence float64 `json:"confidence"`
}

func (s *Server) toolRecall(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args recallArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	budget := domain.DefaultBudget()
	if args.BudgetTokens > 0 {
		budget.Total = args.BudgetTokens
	}

	resp, err := s.recall.Recall(ctx, &app.RecallRequest{
		Query:         args.Query,
		ProjectID:     projectID,
		Budget:        budget,
		AgentID:       s.agentID(),
		IncludeGlobal: projectID == nil,
	})
	if err != nil {
		return nil, err
	}

	hit := resp.Context.Confidence > 0.3 && resp.Candidates > 0
	s.metrics.RecordRecall(hit)

	s.eval.RecordRecall(args.Query, "", resp.Candidates, resp.Context.Confidence, resp.Latency, resp.Context.TokenCount)

	var sourceIDs []string
	if resp.Context.Sources != nil {
		for _, src := range resp.Context.Sources {
			sourceIDs = append(sourceIDs, src.MemoryID.String())
		}
	}

	contextText := resp.Context.Text
	warningsCount := 0

	if s.warningMatcher != nil {
		signals := app.MatchSignals{
			Query:     args.Query,
			QueryEmb:  resp.QueryEmb,
			ProjectID: projectID,
		}
		warnings := s.warningMatcher.Match(ctx, signals)
		if len(warnings) > 0 {
			contextText = formatWarnings(warnings) + "\n" + contextText
			warningsCount = len(warnings)
			s.trackExposure(warnings)
		}
	}

	result := map[string]any{
		"context":              contextText,
		"token_count":          resp.Context.TokenCount,
		"sources_used":         len(resp.Context.Sources),
		"candidates_considered": resp.Candidates,
		"confidence":           resp.Context.Confidence,
		"source_ids":           sourceIDs,
		"warnings_count":       warningsCount,
		"feedback_hint":        "Call mos_feedback(memory_id=source_ids[0], useful=true/false) after using these results",
	}

	return result, nil
}

// --- mos_switch_project ---

type switchProjectArgs struct {
	Project string `json:"project"`
}

func (s *Server) toolSwitchProject(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args switchProjectArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	project, err := s.project.GetBySlug(ctx, args.Project)
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", args.Project, err)
	}

	setActiveProject(&project.ID, project.Slug)

	return map[string]any{
		"status":  "switched",
		"project": project.Slug,
		"name":    project.DisplayName,
		"id":      project.ID,
	}, nil
}

// --- mos_list_projects ---

func (s *Server) toolListProjects(ctx context.Context) (any, error) {
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		items = append(items, map[string]any{
			"slug":         p.Slug,
			"display_name": p.DisplayName,
			"description":  p.Description,
			"is_active":    p.IsActive,
			"id":           p.ID,
		})
	}

	activeID := getActiveProject()
	activeSlug := ""
	if activeID != nil {
		for _, p := range projects {
			if p.ID == *activeID {
				activeSlug = p.Slug
				break
			}
		}
	}

	return map[string]any{
		"projects":       items,
		"active_project": activeSlug,
		"total":          len(items),
	}, nil
}

// --- mos_create_project ---

type createProjectArgs struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	RootPath    string `json:"root_path,omitempty"`
}

func (s *Server) toolCreateProject(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args createProjectArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	project, err := s.project.Create(ctx, args.Slug, args.DisplayName, args.Description, args.RootPath)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":  "created",
		"project": project.Slug,
		"name":    project.DisplayName,
		"id":      project.ID,
	}, nil
}

// --- mos_consolidate ---

type consolidateArgs struct {
	Project string `json:"project,omitempty"`
}

func (s *Server) toolConsolidate(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args consolidateArgs
	if rawArgs != nil && string(rawArgs) != "{}" && string(rawArgs) != "null" {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	result, err := s.consolidate.Run(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Refresh warning matcher cache after consolidation may have created new rules
	if s.warningMatcher != nil {
		s.warningMatcher.LoadRules(ctx, projectID)
	}

	return result, nil
}

// --- mos_feedback ---

type feedbackArgs struct {
	MemoryID string `json:"memory_id"`
	Useful   bool   `json:"useful"`
}

func (s *Server) toolFeedback(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args feedbackArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	memID, err := uuid.Parse(args.MemoryID)
	if err != nil {
		return nil, fmt.Errorf("invalid memory_id: %w", err)
	}

	result, err := s.memory.Feedback(ctx, memID, args.Useful)
	if err != nil {
		return nil, err
	}

	s.eval.RecordFeedback(args.Useful)

	if args.Useful {
		metrics.FeedbackTotal.WithLabelValues("useful").Inc()
	} else {
		metrics.FeedbackTotal.WithLabelValues("not_useful").Inc()
	}

	return result, nil
}

// --- mos_ingest_codebase ---

type ingestCodebaseArgs struct {
	RootPath string `json:"root_path"`
	Project  string `json:"project,omitempty"`
}

func (s *Server) toolIngestCodebase(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args ingestCodebaseArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.RootPath == "" {
		return nil, fmt.Errorf("root_path is required")
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	result, err := s.ingest.IngestGoProject(ctx, args.RootPath, projectID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// --- mos_session_end ---

type sessionEndArgs struct {
	Summary string `json:"summary"`
	Project string `json:"project,omitempty"`
}

func (s *Server) toolSessionEnd(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args sessionEndArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	projectID, err := s.resolveProject(ctx, args.Project)
	if err != nil {
		return nil, err
	}

	content := fmt.Sprintf("SESSION SUMMARY: %s", args.Summary)

	resp, err := s.encode.Encode(ctx, &app.EncodeRequest{
		Content:    content,
		ProjectID:  projectID,
		Importance: 0.9,
		Tags:       []string{"session_summary", "auto"},
	})
	if err != nil {
		return nil, fmt.Errorf("store session summary: %w", err)
	}

	// Prevention analysis: check if exposed warnings prevented anti-patterns in code
	var preventionReport *app.PreventionReport
	s.exposedMu.Lock()
	exposedCopy := make([]app.MatchedWarning, len(s.exposedWarnings))
	copy(exposedCopy, s.exposedWarnings)
	s.exposedWarnings = nil // reset for next session
	s.exposedMu.Unlock()

	if s.preventionAnalyzer != nil && len(exposedCopy) > 0 {
		rootPath := s.findProjectRoot(ctx, projectID)
		if rootPath != "" {
			report, err := s.preventionAnalyzer.Analyze(ctx, exposedCopy, rootPath, s.sessionStartCommit)
			if err != nil {
				s.logger.Warn("prevention analysis failed", "error", err)
			} else {
				preventionReport = report
				s.recordPreventionReport(report)
				metrics.BugsPrevented.Add(float64(report.Prevented))
				metrics.WarningsMissed.Add(float64(report.Ignored))
			}
		}
	}

	s.pendingOps.Add(1)
	go func() {
		defer s.pendingOps.Done()
		bgCtx := context.Background()
		// Note: StoreIfProcedural is already called inside EncodeService.Encode()
		if _, err := s.consolidate.Run(bgCtx, projectID); err != nil {
			s.logger.Warn("post-session consolidation failed", "error", err)
		}
		s.ctxWriter.WriteAll(bgCtx)
		s.ruleGen.GenerateAll(bgCtx)
		if s.warningMatcher != nil {
			s.warningMatcher.LoadRules(bgCtx, projectID)
		}
	}()

	result := map[string]any{
		"status":    "session_saved",
		"memory_id": resp.MemoryID,
		"message":   "Session summary stored. Consolidation + rule generation triggered.",
	}
	if preventionReport != nil {
		result["prevention_report"] = preventionReport
	}

	return result, nil
}

// buildAutoContext assembles rich context from memory at session start.
// Returns session summaries, active decisions, known error patterns,
// and procedural warnings — so the agent has full awareness without calling mos_recall.
func (s *Server) buildAutoContext(ctx context.Context, projectID *uuid.UUID) string {
	var b strings.Builder

	// 1. Recent session summaries
	sessions, err := s.episodic.ListByTags(ctx, projectID, []string{"session_summary"}, 3)
	if err == nil && len(sessions) > 0 {
		b.WriteString("## Recent Sessions\n")
		for _, ep := range sessions {
			content := ep.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			age := formatAge(ep.CreatedAt)
			b.WriteString(fmt.Sprintf("- [%s] %s\n", age, strings.TrimPrefix(content, "SESSION SUMMARY: ")))
		}
		b.WriteString("\n")
	}

	// 2. Decisions and architecture patterns
	decisions, err := s.episodic.ListByTags(ctx, projectID, []string{"decision"}, 5)
	if err == nil && len(decisions) > 0 {
		b.WriteString("## Active Decisions\n")
		for _, ep := range decisions {
			firstLine := mcpFirstLine(ep.Content)
			if len(firstLine) > 150 {
				firstLine = firstLine[:150] + "..."
			}
			b.WriteString(fmt.Sprintf("- %s\n", firstLine))
		}
		b.WriteString("\n")
	}

	// 3. Known error patterns (procedural memories = learned mistakes)
	errors, err := s.episodic.ListByTags(ctx, projectID, []string{"learned_pattern"}, 5)
	if err == nil && len(errors) > 0 {
		b.WriteString("## Known Pitfalls (DO NOT REPEAT)\n")
		for _, ep := range errors {
			firstLine := mcpFirstLine(ep.Content)
			if len(firstLine) > 150 {
				firstLine = firstLine[:150] + "..."
			}
			b.WriteString(fmt.Sprintf("- %s\n", firstLine))
		}
		b.WriteString("\n")
	}

	// 4. Recent errors (high importance, error tag)
	recentErrors, err := s.episodic.ListByTags(ctx, projectID, []string{"error"}, 3)
	if err == nil && len(recentErrors) > 0 {
		b.WriteString("## Recent Errors\n")
		for _, ep := range recentErrors {
			firstLine := mcpFirstLine(ep.Content)
			if len(firstLine) > 150 {
				firstLine = firstLine[:150] + "..."
			}
			age := formatAge(ep.CreatedAt)
			b.WriteString(fmt.Sprintf("- [%s] %s\n", age, firstLine))
		}
		b.WriteString("\n")
	}

	result := b.String()
	if len(result) > 3000 {
		result = result[:3000] + "\n... (truncated)"
	}
	return result
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// resolveProject returns the project UUID for a given slug.
// Falls back to active project if slug is empty.
func (s *Server) resolveProject(ctx context.Context, slug string) (*uuid.UUID, error) {
	if slug == "" {
		return getActiveProject(), nil
	}

	project, err := s.project.GetBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", slug, err)
	}
	return &project.ID, nil
}

func captureHeadCommit(ctx context.Context, projectRoot string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *Server) findProjectRoot(ctx context.Context, projectID *uuid.UUID) string {
	if projectID == nil {
		return ""
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return ""
	}
	for _, p := range projects {
		if p.ID == *projectID {
			return p.RootPath
		}
	}
	return ""
}

func mcpFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}

func formatWarnings(warnings []app.MatchedWarning) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## ⚠ Known Issues (from %d past error patterns)\n\n", len(warnings)))
	for i, w := range warnings {
		label := ""
		if len(w.Rule.FilePaths) > 0 {
			label = "[file:" + w.Rule.FilePaths[0] + "] "
		}
		when := w.Rule.WhenText
		if when == "" {
			when = mcpFirstLine(w.Rule.Content)
		}
		b.WriteString(fmt.Sprintf("%d. %sWHEN: %s\n", i+1, label, when))
		if w.Rule.WatchText != "" {
			b.WriteString(fmt.Sprintf("   WATCH: %s\n", w.Rule.WatchText))
		}
		if w.Rule.DoText != "" {
			b.WriteString(fmt.Sprintf("   DO: %s\n", w.Rule.DoText))
		}
		b.WriteString(fmt.Sprintf("   (matched via %s, confidence=%.2f)\n", w.Signal, w.Confidence))
	}
	return b.String()
}

func (s *Server) trackExposure(warnings []app.MatchedWarning) {
	if len(warnings) == 0 {
		return
	}
	s.exposedMu.Lock()
	s.exposedWarnings = append(s.exposedWarnings, warnings...)
	s.exposedMu.Unlock()

	s.preventionMu.Lock()
	s.totalExposed += len(warnings)
	s.preventionMu.Unlock()

	metrics.WarningsExposed.Add(float64(len(warnings)))
}

func (s *Server) recordPreventionReport(report *app.PreventionReport) {
	if report == nil {
		return
	}
	s.preventionMu.Lock()
	s.totalPrevented += report.Prevented
	s.totalIgnored += report.Ignored
	s.totalNotApplicable += report.NotApplicable
	s.preventionMu.Unlock()
}

func (s *Server) preventionStats() map[string]any {
	s.preventionMu.Lock()
	prevented := s.totalPrevented
	ignored := s.totalIgnored
	exposed := s.totalExposed
	notApplicable := s.totalNotApplicable
	s.preventionMu.Unlock()

	stats := map[string]any{
		"bugs_prevented":    prevented,
		"warnings_exposed":  exposed,
		"warnings_missed":   ignored,
		"not_applicable":    notApplicable,
	}
	if prevented+ignored > 0 {
		stats["prevention_rate"] = float64(prevented) / float64(prevented+ignored)
	}
	return stats
}

// --- mos_predict ---

type predictArgs struct {
	Action          string  `json:"action"`
	ExpectedOutcome string  `json:"expected_outcome"`
	Confidence      float64 `json:"confidence"`
	Domain          string  `json:"domain,omitempty"`
}

func (s *Server) toolPredict(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args predictArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	resp, err := s.prediction.Predict(ctx, &app.PredictRequest{
		Action:     args.Action,
		Expected:   args.ExpectedOutcome,
		Confidence: args.Confidence,
		Domain:     args.Domain,
		ProjectID:  getActiveProject(),
		AgentID:    s.agentID(),
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// --- mos_resolve ---

type resolveArgs struct {
	PredictionID string `json:"prediction_id"`
	Outcome      string `json:"actual_outcome"`
	Success      bool   `json:"success"`
}

func (s *Server) toolResolve(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args resolveArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	predID, err := uuid.Parse(args.PredictionID)
	if err != nil {
		return nil, fmt.Errorf("invalid prediction_id: %w", err)
	}

	resp, err := s.prediction.Resolve(ctx, &app.ResolveRequest{
		PredictionID: predID,
		Outcome:      args.Outcome,
		Success:      args.Success,
	})
	if err != nil {
		return nil, err
	}

	if resp.PredictionError > 0.3 {
		go func() {
			bgCtx := context.Background()
			s.ctxWriter.WriteAll(bgCtx)
			s.ruleGen.GenerateAll(bgCtx)
		}()
	}

	return resp, nil
}

// --- mos_file_context ---

type fileContextArgs struct {
	FilePath           string `json:"file_path"`
	FileContentSnippet string `json:"file_content_snippet,omitempty"`
}

func (s *Server) toolFileContext(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args fileContextArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	projectID := getActiveProject()

	query := fmt.Sprintf("file %s errors patterns decisions", args.FilePath)
	if args.FileContentSnippet != "" {
		query = args.FileContentSnippet + " " + args.FilePath
	}

	resp, err := s.recall.Recall(ctx, &app.RecallRequest{
		Query:         query,
		ProjectID:     projectID,
		Budget:        domain.TokenBudget{Total: 2048},
		AgentID:       "file-context",
		IncludeGlobal: true,
	})
	if err != nil {
		return nil, err
	}

	fileTags := []string{"file:" + args.FilePath}
	fileMemories, _ := s.episodic.ListByTags(ctx, projectID, fileTags, 10)

	var fileSpecific []string
	for _, m := range fileMemories {
		fileSpecific = append(fileSpecific, mcpFirstLine(m.Content))
	}

	var applicableRules []map[string]any
	riskLevel := 0

	if s.warningMatcher != nil {
		signals := app.MatchSignals{
			FilePath:    args.FilePath,
			CodeSnippet: args.FileContentSnippet,
			ProjectID:   projectID,
			QueryEmb:    resp.QueryEmb,
		}
		warnings := s.warningMatcher.Match(ctx, signals)
		if len(warnings) > 0 {
			for _, w := range warnings {
				applicableRules = append(applicableRules, map[string]any{
					"rule":       w.Rule.Content,
					"signal":     w.Signal,
					"confidence": w.Confidence,
				})
			}
			riskLevel = len(warnings)
			s.trackExposure(warnings)
		}
	}

	result := map[string]any{
		"file":              args.FilePath,
		"semantic_context":  resp.Context.Text,
		"file_memories":     len(fileSpecific),
		"file_errors":       fileSpecific,
		"confidence":        resp.Context.Confidence,
		"total_candidates":  resp.Candidates,
		"applicable_rules":  applicableRules,
		"risk_level":        riskLevel,
	}

	if len(fileSpecific) > 0 {
		result["warning"] = fmt.Sprintf("Found %d error patterns specifically for this file. Review before editing.", len(fileSpecific))
	}

	return result, nil
}

// --- mos_benchmark ---

func (s *Server) toolBenchmark(ctx context.Context) (any, error) {
	report, err := s.benchmark.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("benchmark failed: %w", err)
	}

	return map[string]any{
		"formatted_report": report.Formatted,
		"aggregate":        report.Aggregate,
		"baseline":         report.Baseline,
		"conclusion":       report.Conclusion,
		"by_category":      report.ByCategory,
		"scenario_count":   report.Scenarios,
		"duration_ms":      report.Duration.Milliseconds(),
	}, nil
}

// --- mos_study_project ---

func (s *Server) toolStudyProject(ctx context.Context) (any, error) {
	projID := getActiveProject()
	if projID == nil {
		return nil, fmt.Errorf("no active project — call mos_init first")
	}

	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	var rootPath string
	for _, p := range projects {
		if p.ID == *projID {
			rootPath = p.RootPath
			break
		}
	}
	if rootPath == "" {
		return nil, fmt.Errorf("active project has no root_path — reinitialize with mos_init")
	}

	result, err := s.study.Study(ctx, rootPath, projID)
	if err != nil {
		return nil, fmt.Errorf("study failed: %w", err)
	}

	return map[string]any{
		"files_read":       result.FilesRead,
		"code_files":       result.CodeFiles,
		"doc_files":        result.DocFiles,
		"memories_created": result.MemoriesCreated,
		"skipped":          result.Skipped,
		"by_language":      result.ByLanguage,
		"files_studied":    result.Files,
		"duration_ms":      result.Duration.Milliseconds(),
		"warnings":         result.Warnings,
	}, nil
}

// --- mos_metrics ---

func (s *Server) toolMetrics(ctx context.Context) (any, error) {
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	report := s.metrics.Report(ctx, projects)

	prevention := s.preventionStats()

	result := map[string]any{
		"learning_report":       report,
		"prediction_calibration": s.prediction.GetCalibration(),
		"pending_predictions":   s.prediction.PendingCount(),
		"prevention":            prevention,
	}
	return result, nil
}

// --- mos_research ---

func (s *Server) toolResearch(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	var args struct {
		Query   string   `json:"query"`
		Sources []string `json:"sources,omitempty"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	req := &app.ResearchRequest{
		Query:      args.Query,
		Sources:    args.Sources,
		MaxResults: 5,
	}

	result, err := s.research.Research(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("research failed: %w", err)
	}

	response := map[string]any{
		"query":       result.Query,
		"synthesis":   result.Synthesis,
		"source_count": len(result.Sources),
		"sources":     result.Sources,
		"duration_ms": result.Duration.Milliseconds(),
	}

	return response, nil
}

// --- mos_evaluate ---

func (s *Server) toolEvaluate(ctx context.Context, _ json.RawMessage) (any, error) {
	report := s.eval.Evaluate()
	return report, nil
}

// --- mos_analogize ---

func (s *Server) toolAnalogize(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Query         string `json:"query"`
		SourceProject string `json:"source_project,omitempty"`
		TargetProject string `json:"target_project,omitempty"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	req := &app.AnalogizeRequest{
		Query: args.Query,
		Limit: 5,
	}

	if args.SourceProject != "" {
		srcID, err := s.resolveProject(ctx, args.SourceProject)
		if err != nil {
			return nil, err
		}
		req.SourceProject = srcID
	}
	if args.TargetProject != "" {
		tgtID, err := s.resolveProject(ctx, args.TargetProject)
		if err != nil {
			return nil, err
		}
		req.TargetProject = tgtID
	}

	resp, err := s.analogize.Analogize(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Analogies) == 0 {
		return map[string]any{
			"message":   "No cross-project analogies found. This works best with multiple projects that have stored memories.",
			"analogies": []any{},
		}, nil
	}

	return map[string]any{
		"analogies": resp.Analogies,
		"count":     len(resp.Analogies),
	}, nil
}

// --- mos_meta ---

func (s *Server) toolMeta(ctx context.Context) (any, error) {
	projID := getActiveProject()
	report, err := s.meta.Assess(ctx, projID)
	if err != nil {
		return nil, fmt.Errorf("meta assessment failed: %w", err)
	}
	return report, nil
}

// --- mos_track_outcome ---

func (s *Server) toolTrackOutcome(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Description string `json:"description"`
		Success     bool   `json:"success"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Description == "" {
		return nil, fmt.Errorf("description is required")
	}

	projID := getActiveProject()
	err := s.proceduralSvc.TrackOutcome(ctx, args.Description, args.Success, projID)
	if err != nil {
		return nil, fmt.Errorf("track outcome: %w", err)
	}

	outcome := "failure"
	if args.Success {
		outcome = "success"
	}

	return map[string]any{
		"status":  "recorded",
		"outcome": outcome,
	}, nil
}

func (s *Server) toolCurate(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Topic   string   `json:"topic"`
		Depth   string   `json:"depth"`
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	if s.scheduler == nil {
		return nil, fmt.Errorf("knowledge scheduler not configured")
	}

	findings, err := s.scheduler.RunOnce(ctx, args.Topic, args.Domains)
	if err != nil {
		return nil, fmt.Errorf("curate: %w", err)
	}

	summaries := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		summaries = append(summaries, map[string]any{
			"title":         f.Title,
			"source":        f.Source,
			"quality_score": f.QualityScore,
			"domain":        f.Domain,
			"url":           f.URL,
			"key_findings":  f.KeyFindings,
		})
	}

	return map[string]any{
		"topic":    args.Topic,
		"findings": len(findings),
		"results":  summaries,
	}, nil
}

func (s *Server) toolFuse(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Query            string   `json:"query"`
		ExternalEvidence []string `json:"external_evidence"`
		Rerank           bool     `json:"rerank"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	if s.fusion == nil {
		return nil, fmt.Errorf("fusion engine not configured")
	}

	projID := getActiveProject()
	resp, err := s.fusion.Fuse(ctx, &app.FusionRequest{
		Query:            args.Query,
		ExternalEvidence: args.ExternalEvidence,
		ProjectID:        projID,
		Rerank:           args.Rerank,
	})
	if err != nil {
		return nil, fmt.Errorf("fuse: %w", err)
	}

	facts := make([]map[string]any, 0, len(resp.Facts))
	for _, f := range resp.Facts {
		facts = append(facts, map[string]any{
			"content":    f.Content,
			"belief":     f.Belief,
			"uncertainty": f.Uncertainty,
			"provenance": f.Provenance,
			"agreement":  f.Agreement,
		})
	}

	return map[string]any{
		"insight":    resp.Insight,
		"confidence": resp.Confidence,
		"conflict":   resp.Conflict,
		"facts":      facts,
		"sources":    resp.Sources,
	}, nil
}

func (s *Server) toolCite(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	id, err := uuid.Parse(args.MemoryID)
	if err != nil {
		return nil, fmt.Errorf("invalid memory_id: %w", err)
	}

	mem, err := s.episodic.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}

	provenance := map[string]any{
		"id":          mem.ID.String(),
		"content":     mem.Content,
		"importance":  mem.Importance,
		"confidence":  mem.Confidence,
		"tags":        mem.Tags,
		"created_at":  mem.CreatedAt,
		"agent_id":    mem.AgentID,
	}

	if mem.Metadata != nil {
		if doi, ok := mem.Metadata["doi"]; ok {
			provenance["doi"] = doi
		}
		if url, ok := mem.Metadata["url"]; ok {
			provenance["url"] = url
		}
		if authors, ok := mem.Metadata["authors"]; ok {
			provenance["authors"] = authors
		}
		if source, ok := mem.Metadata["source"]; ok {
			provenance["source"] = source
		}
		if quality, ok := mem.Metadata["quality_score"]; ok {
			provenance["quality_score"] = quality
		}
	}

	return provenance, nil
}

func (s *Server) toolABTest(ctx context.Context) (any, error) {
	report, err := s.abBenchmark.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("A/B test failed: %w", err)
	}
	return map[string]any{
		"formatted_report":  report.Formatted,
		"warning_precision": report.WarningPrecision,
		"warning_recall":    report.WarningRecall,
		"prevention_lift":   report.PreventionLift,
		"scenarios":         report.Scenarios,
		"mean_latency_us":   report.MeanMatchLatency.Microseconds(),
		"p95_latency_us":    report.P95MatchLatency.Microseconds(),
		"by_category":       report.ByCategory,
		"duration_ms":       report.Duration.Milliseconds(),
	}, nil
}

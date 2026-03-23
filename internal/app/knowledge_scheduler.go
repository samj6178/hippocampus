package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// KnowledgeScheduler runs background research across all agents on a timer.
// It identifies knowledge gaps via MetaService and distributes queries.
type KnowledgeScheduler struct {
	agents        []*KnowledgeAgent
	meta          *MetaService
	interval      time.Duration
	maxConcurrent int
	stopCh        chan struct{}
	logger        *slog.Logger
}

func NewKnowledgeScheduler(
	agents []*KnowledgeAgent,
	meta *MetaService,
	interval time.Duration,
	maxConcurrent int,
	logger *slog.Logger,
) *KnowledgeScheduler {
	if interval <= 0 {
		interval = 4 * time.Hour
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	return &KnowledgeScheduler{
		agents:        agents,
		meta:          meta,
		interval:      interval,
		maxConcurrent: maxConcurrent,
		stopCh:        make(chan struct{}),
		logger:        logger,
	}
}

// Start begins the background research loop.
func (ks *KnowledgeScheduler) Start(ctx context.Context) {
	ks.logger.Info("knowledge scheduler starting",
		"agents", len(ks.agents),
		"interval", ks.interval,
		"max_concurrent", ks.maxConcurrent,
	)

	time.Sleep(30 * time.Second)
	ks.runCycle(ctx)

	ticker := time.NewTicker(ks.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ks.stopCh:
			return
		case <-ticker.C:
			ks.runCycle(ctx)
		}
	}
}

func (ks *KnowledgeScheduler) Stop() {
	close(ks.stopCh)
}

func (ks *KnowledgeScheduler) runCycle(ctx context.Context) {
	start := time.Now()
	ks.logger.Info("knowledge cycle starting")

	queries := ks.generateQueries(ctx)
	if len(queries) == 0 {
		ks.logger.Info("no knowledge gaps found, skipping cycle")
		return
	}

	sem := make(chan struct{}, ks.maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	totalFindings := 0

	for _, aq := range queries {
		wg.Add(1)
		go func(agent *KnowledgeAgent, query string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			findings, err := agent.Research(ctx, query, nil, 5)
			if err != nil {
				ks.logger.Warn("agent research failed", "agent", agent.AgentName(), "error", err)
				return
			}
			mu.Lock()
			totalFindings += len(findings)
			mu.Unlock()
		}(aq.agent, aq.query)
	}

	wg.Wait()

	ks.logger.Info("knowledge cycle completed",
		"queries", len(queries),
		"findings", totalFindings,
		"duration", time.Since(start),
	)
}

type agentQuery struct {
	agent *KnowledgeAgent
	query string
}

func (ks *KnowledgeScheduler) generateQueries(ctx context.Context) []agentQuery {
	var queries []agentQuery

	if ks.meta != nil {
		report, err := ks.meta.Assess(ctx, nil)
		if err == nil && len(report.KnowledgeGaps) > 0 {
			for _, gap := range report.KnowledgeGaps {
				for _, agent := range ks.agents {
					if len(agent.queryTemplates) > 0 {
						query := agent.queryTemplates[0]
						if len(query) > 0 {
							queries = append(queries, agentQuery{
								agent: agent,
								query: formatTemplate(query, gap.Description),
							})
						}
					}
				}
			}
			if len(queries) > 0 {
				return queries
			}
		}
	}

	defaultTopics := []string{
		"retrieval augmented generation optimization",
		"vector database indexing strategies",
		"memory systems for AI agents",
		"knowledge graph construction",
		"long-term memory in neural networks",
	}

	for i, topic := range defaultTopics {
		agentIdx := i % len(ks.agents)
		queries = append(queries, agentQuery{
			agent: ks.agents[agentIdx],
			query: topic,
		})
	}

	return queries
}

func formatTemplate(template, topic string) string {
	if idx := len(template); idx > 0 {
		return template[:0] + topic
	}
	return topic
}

// RunOnce triggers a single research cycle (for manual MCP invocation).
func (ks *KnowledgeScheduler) RunOnce(ctx context.Context, topic string, domains []string) ([]ScientificFinding, error) {
	var findings []ScientificFinding
	sem := make(chan struct{}, ks.maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, agent := range ks.agents {
		if len(domains) > 0 && !containsStr(domains, agent.AgentDomain()) {
			continue
		}
		wg.Add(1)
		go func(a *KnowledgeAgent) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := a.Research(ctx, topic, nil, 5)
			if err != nil {
				ks.logger.Warn("on-demand research failed", "agent", a.AgentName(), "error", err)
				return
			}
			mu.Lock()
			findings = append(findings, result...)
			mu.Unlock()
		}(agent)
	}

	wg.Wait()
	return findings, nil
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

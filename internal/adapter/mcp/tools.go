package mcp

// tools returns the MCP tool definitions exposed by Hippocampus MOS.
func tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "mos_init",
			"description": "Initialize Hippocampus for the current workspace. CALL THIS FIRST in every new conversation. It auto-detects the project from the workspace path, creates it if new, switches context, and returns the current project state. If the workspace doesn't match any known project, a new project is created automatically.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace_path": map[string]any{
						"type":        "string",
						"description": "Absolute path to the current workspace/project root.",
					},
					"project_name": map[string]any{
						"type":        "string",
						"description": "Human-readable name for the project (used only when creating a new project).",
					},
				},
				"required": []string{"workspace_path"},
			},
		},
		{
			"name":        "mos_remember",
			"description": "Store a memory in Hippocampus MOS. Use this to save important facts, decisions, patterns, errors encountered, or anything worth remembering across sessions. Memories are automatically embedded, scored, and stored in the appropriate tier.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The content to remember. Be specific and include context.",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug (e.g. 'energy-monitor'). Omit for global memory.",
					},
					"importance": map[string]any{
						"type":        "number",
						"description": "Importance score 0.0-1.0. Higher = more likely to be recalled. Default 0.5.",
					},
					"tags": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
						"description": "Optional tags for categorization (e.g. ['architecture', 'decision']).",
					},
				},
				"required": []string{"content"},
			},
		},
		{
			"name":        "mos_learn_error",
			"description": "CALL THIS AUTOMATICALLY when any error occurs (compilation failure, test failure, runtime error, deployment failure). Structures the error as a preventable pattern and stores it as a high-importance warning that appears in the project's context file. Next time anyone works on this project, they'll see the warning BEFORE making the same mistake.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"error_message": map[string]any{
						"type":        "string",
						"description": "The exact error message or description of what went wrong.",
					},
					"context": map[string]any{
						"type":        "string",
						"description": "What you were trying to do when the error occurred.",
					},
					"root_cause": map[string]any{
						"type":        "string",
						"description": "Why it happened (the real reason, not just the symptom).",
					},
					"fix": map[string]any{
						"type":        "string",
						"description": "What fixed it or how to fix it.",
					},
					"prevention": map[string]any{
						"type":        "string",
						"description": "How to prevent this from happening again.",
					},
					"file_path": map[string]any{
						"type":        "string",
						"description": "File path where the error occurred.",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug. Omit to use active project.",
					},
				},
				"required": []string{"error_message", "root_cause", "fix"},
			},
		},
		{
			"name":        "mos_recall",
			"description": "Recall relevant memories for a given task or query. Returns assembled context from all memory tiers (working, episodic, semantic, procedural) optimized for your token budget. Use this at the start of any task to load relevant context.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "What you need to know. Describe the task or question.",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug to search in. Omit to search all projects.",
					},
					"budget_tokens": map[string]any{
						"type":        "integer",
						"description": "Maximum tokens for assembled context. Default 4096.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "mos_switch_project",
			"description": "Switch the active project context. This clears working memory and sets the default project for subsequent remember/recall operations.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug to switch to (e.g. 'energy-monitor').",
					},
				},
				"required": []string{"project"},
			},
		},
		{
			"name":        "mos_list_projects",
			"description": "List all registered projects with their memory statistics.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_create_project",
			"description": "Register a new project for memory namespacing. Each project gets isolated episodic and procedural memory while sharing global semantic knowledge.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "URL-safe project identifier (e.g. 'energy-monitor', 'pikk').",
					},
					"display_name": map[string]any{
						"type":        "string",
						"description": "Human-readable project name.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Brief project description.",
					},
					"root_path": map[string]any{
						"type":        "string",
						"description": "Filesystem path to the project root.",
					},
				},
				"required": []string{"slug", "display_name"},
			},
		},
		{
			"name":        "mos_feedback",
			"description": "Report whether a recalled memory was useful or not. This adjusts the memory's importance score: useful memories become more likely to be recalled in the future, useless ones fade. Use this after completing a task to help the system learn which memories are valuable.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "UUID of the memory to provide feedback on.",
					},
					"useful": map[string]any{
						"type":        "boolean",
						"description": "True if the memory was helpful, false if it was irrelevant or misleading.",
					},
				},
				"required": []string{"memory_id", "useful"},
			},
		},
		{
			"name":        "mos_ingest_codebase",
			"description": "Ingest a codebase into memory. Walks the directory, extracts functions/structs/classes with full source code bodies, and stores them as semantic memories. Supports Go (AST), TypeScript, Python, Rust, C++, Java, Ruby, C#. Deduplicates on re-run. After ingestion, any agent can recall code via mos_recall.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root_path": map[string]any{
						"type":        "string",
						"description": "Absolute path to the project root directory to scan.",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug to associate ingested memories with.",
					},
				},
				"required": []string{"root_path"},
			},
		},
		{
			"name":        "mos_session_end",
			"description": "Call this when the conversation is ending or when the user says goodbye. Summarizes the current session's work and stores it as a high-importance episodic memory. This ensures continuity between sessions — the next session will automatically recall what was done.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "Summary of what was accomplished in this session: decisions made, code changed, problems solved, next steps.",
					},
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug. Omit to use active project.",
					},
				},
				"required": []string{"summary"},
			},
		},
		{
			"name":        "mos_health",
			"description": "Check the health of the memory system. Returns embedding status, database state, memory counts, and recommendations. Use this to diagnose issues.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_consolidate",
			"description": "Trigger memory consolidation (episodic -> semantic). Clusters similar episodic memories, promotes them to semantic facts, and marks sources as consolidated. Run this periodically or when recall quality degrades due to duplicate episodic memories.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{
						"type":        "string",
						"description": "Project slug to consolidate. Omit to consolidate active project.",
					},
				},
			},
		},
		{
			"name":        "mos_predict",
			"description": "Register a prediction BEFORE performing an action. The system will track your prediction accuracy and warn about overconfidence. After the action completes, call mos_resolve with the actual outcome. High prediction errors (surprises) create stronger memories — this is how the system learns what's unexpected and important.\n\nNeuroscience: mirrors hippocampal prediction error (dopamine signal). δ = |predicted - actual| determines memory encoding strength.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "What you're about to do (e.g., 'deploy to production', 'refactor auth module', 'add new API endpoint').",
					},
					"expected_outcome": map[string]any{
						"type":        "string",
						"description": "What you expect will happen (e.g., 'deployment succeeds', 'tests pass', 'no breaking changes').",
					},
					"confidence": map[string]any{
						"type":        "number",
						"description": "How confident are you? 0.0 to 1.0. Be honest — the system tracks calibration.",
					},
					"domain": map[string]any{
						"type":        "string",
						"description": "Knowledge domain (e.g., 'deployment', 'testing', 'refactoring', 'database', 'api'). Used for per-domain calibration tracking.",
					},
				},
				"required": []string{"action", "expected_outcome", "confidence"},
			},
		},
		{
			"name":        "mos_resolve",
			"description": "Resolve a prediction with actual outcome. Computes prediction error (surprise signal), uses it to determine memory importance, and updates domain calibration. High surprise → strong memory encoding. This is the learning loop.\n\nMath: importance = 0.5 × (1 + 1.5 × |predicted_confidence - actual|). Memories from surprising outcomes are encoded 2-3x stronger.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prediction_id": map[string]any{
						"type":        "string",
						"description": "The prediction_id returned by mos_predict.",
					},
					"actual_outcome": map[string]any{
						"type":        "string",
						"description": "What actually happened.",
					},
					"success": map[string]any{
						"type":        "boolean",
						"description": "Did the action succeed as predicted?",
					},
				},
				"required": []string{"prediction_id", "actual_outcome", "success"},
			},
		},
		{
			"name":        "mos_file_context",
			"description": "Get memories relevant to a specific file you're about to edit. Returns errors that happened in this file, related patterns, and relevant knowledge. Call this when opening an important file for editing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Relative or absolute path to the file being edited.",
					},
					"file_content_snippet": map[string]any{
						"type":        "string",
						"description": "Optional: first ~200 chars of the file for better semantic matching.",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			"name":        "mos_research",
			"description": "Autonomous research agent. Searches arxiv, GitHub, and Hacker News for knowledge relevant to a query, then synthesizes findings via LLM. Use this to find state-of-the-art approaches, competitor analysis, best practices, and recent developments. Results are synthesized into actionable insights.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "What to research (e.g., 'AI agent memory systems state of the art', 'vector similarity search optimization techniques').",
					},
					"sources": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Sources to search: 'arxiv', 'github', 'hackernews'. Default: all three.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "mos_study_project",
			"description": "Study the current project deeply. Reads README, .mdc rules, CLAUDE.md, docker-compose, Makefile, go.mod, docs/*.md and stores their content as memories. After this, any agent in any session can recall this knowledge. Secrets (passwords, API keys) are automatically REDACTED before storage. Call this when entering a new project for the first time.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_benchmark",
			"description": "Run the reproducible evaluation benchmark. Seeds known memories, runs 12 test queries (error prevention, knowledge recall, negative rejection, semantic paraphrase), measures precision/recall/F1/MRR, compares against random baseline. This is the PROOF that Hippocampus works. Output: formatted scientific report.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_evaluate",
			"description": "Run the evaluation framework. Returns formal metrics: recall precision (% of recalls marked useful), Brier calibration score, learning curve (is quality improving over time?), per-domain breakdown. This is the PROOF that Hippocampus makes agents better.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_analogize",
			"description": "Find cross-project analogies. Given a query or pattern, search ALL projects for structurally similar knowledge that could transfer. Based on Structure Mapping Theory (Gentner, 1983). Use this when solving a problem that might have been solved in another project.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The pattern, problem, or concept to find analogies for.",
					},
					"source_project": map[string]any{
						"type":        "string",
						"description": "Optional: restrict source to this project slug.",
					},
					"target_project": map[string]any{
						"type":        "string",
						"description": "Optional: restrict search target to this project slug.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "mos_meta",
			"description": "Self-assessment of memory quality. Returns: memory distribution (episodic/semantic/procedural counts), domain calibration (where the system is over/under-confident), knowledge gaps (areas needing more data), strengths, weaknesses, and actionable recommendations. This is the system 'thinking about its own thinking' (metacognition).",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_track_outcome",
			"description": "Track success/failure of a procedure. After following a stored procedure (deployment steps, build workflow, etc.), report whether it worked. This trains procedural memory over time — successful procedures gain confidence, failed ones get flagged.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"description": map[string]any{
						"type":        "string",
						"description": "What procedure was followed (e.g., 'deployed to production using docker-compose').",
					},
					"success": map[string]any{
						"type":        "boolean",
						"description": "Did the procedure succeed?",
					},
				},
				"required": []string{"description", "success"},
			},
		},
		{
			"name":        "mos_metrics",
			"description": "View learning metrics and system intelligence report. Shows: memory counts, recall hit rate, errors learned, knowledge coverage by topic, learning velocity, auto-generated rules count. Use this to understand how much the system has learned.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mos_curate",
			"description": "Deep research across 6 specialized knowledge agents (math, CS/ML, physics, biology, systems, ML/DS-Stanford). Searches arXiv, Semantic Scholar, PubMed, GitHub, HN, Papers With Code. Extracts structured findings via LLM, scores quality, stores in MOS. Use for targeted knowledge acquisition on any scientific topic.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{
						"type":        "string",
						"description": "Research topic to investigate.",
					},
					"depth": map[string]any{
						"type":        "string",
						"description": "Research depth: 'quick' (3 results/source), 'deep' (5), 'exhaustive' (10).",
						"enum":        []string{"quick", "deep", "exhaustive"},
					},
					"domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional: filter to specific domains (mathematics, computer_science, physics_engineering, biology_chemistry, systems_practice, ml_data_science).",
					},
				},
				"required": []string{"topic"},
			},
		},
		{
			"name":        "mos_fuse",
			"description": "PRIORITY TOOL. Combines MOS persistent memory with YOUR OWN web search results using Dempster-Shafer evidence theory. Pass what you found via web search as external_evidence — MOS will fuse it with stored code knowledge, past decisions, error patterns, and cross-project insights. Returns ranked facts with calibrated belief scores and synthesized insight. Use this for any complex question where you want MOS context + your web findings combined into one answer.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The question to answer.",
					},
					"external_evidence": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Evidence from YOUR OWN web search (Cursor/Claude Code web results). MOS will fuse this with stored memory for a combined answer.",
					},
					"rerank": map[string]any{
						"type":        "boolean",
						"description": "Enable cross-encoder LLM reranking for higher precision (slower).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "mos_cite",
			"description": "Get full provenance for a memory: DOI, authors, source, quality score, venue. Use to verify the origin and reliability of any stored knowledge.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "UUID of the memory to get provenance for.",
					},
				},
				"required": []string{"memory_id"},
			},
		},
		{
			"name":        "mos_ab_test",
			"description": "Run A/B test benchmark. Tests 12 coding scenarios with known anti-patterns in two conditions: control (warnings OFF) and treatment (warnings ON). Measures warning precision/recall, prevention lift, and matching latency. Deterministic, no DB or LLM required.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

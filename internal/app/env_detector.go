package app

import (
	"os"
	"path/filepath"
)

// AgentEnvironment identifies which AI coding tool is being used.
type AgentEnvironment string

const (
	EnvCursor    AgentEnvironment = "cursor"
	EnvClaudeCode AgentEnvironment = "claude_code"
	EnvVSCode    AgentEnvironment = "vscode"
	EnvWindsurf  AgentEnvironment = "windsurf"
	EnvUnknown   AgentEnvironment = "unknown"
)

// DetectedEnvironments holds all detected AI tool environments for a project.
type DetectedEnvironments struct {
	Environments []AgentEnvironment
	ProjectRoot  string
}

// DetectEnvironments checks for known AI tool directories in the project root.
// A project may have multiple environments (e.g., both .cursor/ and .claude/).
func DetectEnvironments(projectRoot string) *DetectedEnvironments {
	result := &DetectedEnvironments{ProjectRoot: projectRoot}

	checks := []struct {
		dir string
		env AgentEnvironment
	}{
		{".cursor", EnvCursor},
		{".claude", EnvClaudeCode},
		{".vscode", EnvVSCode},
		{".windsurf", EnvWindsurf},
	}

	for _, c := range checks {
		if dirExists(filepath.Join(projectRoot, c.dir)) {
			result.Environments = append(result.Environments, c.env)
		}
	}

	if fileExists(filepath.Join(projectRoot, "CLAUDE.md")) && !containsEnv(result.Environments, EnvClaudeCode) {
		result.Environments = append(result.Environments, EnvClaudeCode)
	}

	if len(result.Environments) == 0 {
		result.Environments = []AgentEnvironment{EnvUnknown}
	}

	return result
}

// DetectEnvFromClientName maps MCP client info names to environment types.
func DetectEnvFromClientName(name string) AgentEnvironment {
	switch {
	case name == "cursor" || name == "Cursor":
		return EnvCursor
	case name == "claude-code" || name == "Claude Code" || name == "claude":
		return EnvClaudeCode
	case name == "vscode" || name == "Visual Studio Code":
		return EnvVSCode
	case name == "windsurf" || name == "Windsurf":
		return EnvWindsurf
	default:
		return EnvUnknown
	}
}

func containsEnv(envs []AgentEnvironment, target AgentEnvironment) bool {
	for _, e := range envs {
		if e == target {
			return true
		}
	}
	return false
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

package core

import "path/filepath"

type agentPath struct {
	Agent AgentKind
	Path  string
}

// SkillRoots.
func SkillRoots(scope Scope, cwd string) []agentPath {
	if scope == ScopeUser {
		h := HomeDir()
		return []agentPath{
			{AgentAgents, filepath.Join(h, ".agents/skills")},
			{AgentGrok, filepath.Join(h, ".grok/skills")},
			{AgentDevin, filepath.Join(h, ".config/devin/skills")},
			{AgentCognition, filepath.Join(h, ".config/cognition/skills")},
			{AgentPi, filepath.Join(h, ".pi/agent/skills")},
			{AgentOmp, filepath.Join(h, ".omp/agent/skills")},
			{AgentKiro, filepath.Join(h, ".kiro/skills")},
			{AgentOpenCode, filepath.Join(h, ".config/opencode/skills")},
			{AgentClaude, filepath.Join(h, ".claude/skills")},
			{AgentCursor, filepath.Join(h, ".cursor/skills")},
		}
	}
	root := ProjectRoot(cwd)
	if root == "" {
		return nil
	}
	return []agentPath{
		{AgentAgents, filepath.Join(root, ".agents/skills")},
		{AgentGrok, filepath.Join(root, ".grok/skills")},
		{AgentDevin, filepath.Join(root, ".devin/skills")},
		{AgentCognition, filepath.Join(root, ".cognition/skills")},
		{AgentOmp, filepath.Join(root, ".omp/skills")},
		{AgentPi, filepath.Join(root, ".pi/skills")},
		{AgentKiro, filepath.Join(root, ".kiro/skills")},
		{AgentOpenCode, filepath.Join(root, ".opencode/skills")},
		{AgentClaude, filepath.Join(root, ".claude/skills")},
		{AgentCursor, filepath.Join(root, ".cursor/skills")},
	}
}

// McpConfigPaths.
func McpConfigPaths(scope Scope, cwd string) []agentPath {
	if scope == ScopeUser {
		h := HomeDir()
		return []agentPath{
			{AgentAgents, filepath.Join(h, ".agents/mcp.json")},
			{AgentGrok, filepath.Join(h, ".grok/config.toml")},
			{AgentDevin, filepath.Join(h, ".config/devin/mcp_config.json")},
			{AgentOmp, filepath.Join(h, ".omp/agent/mcp.json")},
			{AgentPi, filepath.Join(h, ".pi/agent/mcp.json")},
			{AgentKiro, filepath.Join(h, ".kiro/settings/mcp.json")},
			{AgentOpenCode, filepath.Join(h, ".config/opencode/opencode.json")},
			{AgentClaude, filepath.Join(h, ".claude.json")},
			{AgentCursor, filepath.Join(h, ".cursor/mcp.json")},
		}
	}
	root := ProjectRoot(cwd)
	if root == "" {
		return nil
	}
	return []agentPath{
		{AgentAgents, filepath.Join(root, ".mcp.json")},
		{AgentGrok, filepath.Join(root, ".grok/config.toml")},
		{AgentOmp, filepath.Join(root, ".omp/mcp.json")},
		{AgentPi, filepath.Join(root, ".pi/mcp.json")},
		{AgentKiro, filepath.Join(root, ".kiro/settings/mcp.json")},
		{AgentOpenCode, filepath.Join(root, "opencode.json")},
		{AgentCursor, filepath.Join(root, ".cursor/mcp.json")},
	}
}

func CanonicalSkillsDir(scope Scope, cwd string) string {
	if scope == ScopeUser {
		return filepath.Join(HomeDir(), ".agents/skills")
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, ".agents/skills")
	}
	return ""
}

func CanonicalMcpPath(scope Scope, cwd string) string {
	if scope == ScopeUser {
		return filepath.Join(HomeDir(), ".agents/mcp.json")
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, ".mcp.json")
	}
	return ""
}

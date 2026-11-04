package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type agentPath struct {
	Agent AgentKind
	Path  string
}

// LocalRoot is where skills and MCP configs the user wrote live.
// Installed copies live under .agents and are the originals for those items.
func LocalRoot(scope Scope, cwd string) string {
	if scope == ScopeUser {
		return filepath.Join(HomeDir(), ".synca", "local")
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, ".synca", "local")
	}
	return ""
}

func InstalledRoot(scope Scope, cwd string) string {
	if scope == ScopeUser {
		return filepath.Join(HomeDir(), ".agents")
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, ".agents")
	}
	return ""
}

// pathsEqual compares cleaned paths. An empty path never matches.
func pathsEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// SkillRoots lists the skill directories synca reads and writes.
// Installed (.agents/skills) comes first, then local (.synca/local/skills).
// Both are tagged agents. The original for one skill is only one of them.
// Pi (~/.pi) and OMP (~/.omp) are not targets.
func SkillRoots(scope Scope, cwd string) []agentPath {
	if scope == ScopeUser {
		h := HomeDir()
		return []agentPath{
			{AgentAgents, InstalledSkillsDir(ScopeUser, "")},
			{AgentAgents, LocalSkillsDir(ScopeUser, "")},
			{AgentGrok, filepath.Join(h, ".grok", "skills")},
			{AgentDevin, filepath.Join(h, ".config", "devin", "skills")},
			{AgentKiro, filepath.Join(h, ".kiro", "skills")},
			{AgentOpenCode, filepath.Join(h, ".config", "opencode", "skills")},
			{AgentClaude, filepath.Join(h, ".claude", "skills")},
			{AgentCursor, filepath.Join(h, ".cursor", "skills")},
		}
	}
	root := ProjectRoot(cwd)
	if root == "" {
		return nil
	}
	return []agentPath{
		{AgentAgents, InstalledSkillsDir(ScopeProject, cwd)},
		{AgentAgents, LocalSkillsDir(ScopeProject, cwd)},
		{AgentGrok, filepath.Join(root, ".grok", "skills")},
		{AgentDevin, filepath.Join(root, ".devin", "skills")},
		{AgentKiro, filepath.Join(root, ".kiro", "skills")},
		{AgentOpenCode, filepath.Join(root, ".opencode", "skills")},
		{AgentClaude, filepath.Join(root, ".claude", "skills")},
		{AgentCursor, filepath.Join(root, ".cursor", "skills")},
	}
}

// McpConfigPaths lists MCP config files. Installed and local hubs come first,
// both tagged agents. Project .mcp.json is Claude's file, written from the hub
// that owns each server. It is not a hub.
func McpConfigPaths(scope Scope, cwd string) []agentPath {
	if scope == ScopeUser {
		h := HomeDir()
		return []agentPath{
			{AgentAgents, InstalledMcpPath(ScopeUser, "")},
			{AgentAgents, LocalMcpPath(ScopeUser, "")},
			{AgentGrok, filepath.Join(h, ".grok", "config.toml")},
			{AgentDevin, filepath.Join(h, ".config", "devin", "mcp_config.json")},
			{AgentKiro, filepath.Join(h, ".kiro", "settings", "mcp.json")},
			{AgentOpenCode, filepath.Join(h, ".config", "opencode", "opencode.json")},
			{AgentClaude, filepath.Join(h, ".claude.json")},
			{AgentCursor, filepath.Join(h, ".cursor", "mcp.json")},
		}
	}
	root := ProjectRoot(cwd)
	if root == "" {
		return nil
	}
	return []agentPath{
		{AgentAgents, InstalledMcpPath(ScopeProject, cwd)},
		{AgentAgents, LocalMcpPath(ScopeProject, cwd)},
		{AgentClaude, filepath.Join(root, ".mcp.json")},
		{AgentGrok, filepath.Join(root, ".grok", "config.toml")},
		{AgentKiro, filepath.Join(root, ".kiro", "settings", "mcp.json")},
		{AgentOpenCode, filepath.Join(root, "opencode.json")},
		{AgentCursor, filepath.Join(root, ".cursor", "mcp.json")},
	}
}

func LocalSkillsDir(scope Scope, cwd string) string {
	root := LocalRoot(scope, cwd)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "skills")
}

func InstalledSkillsDir(scope Scope, cwd string) string {
	root := InstalledRoot(scope, cwd)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "skills")
}

func LocalMcpPath(scope Scope, cwd string) string {
	root := LocalRoot(scope, cwd)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "mcp.json")
}

func InstalledMcpPath(scope Scope, cwd string) string {
	root := InstalledRoot(scope, cwd)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "mcp.json")
}

// SkillOriginalDir is the directory that owns a skill.
// A remote install lives under .agents. Anything else lives under .synca/local.
func SkillOriginalDir(scope Scope, cwd string, remote bool) string {
	if remote {
		return InstalledSkillsDir(scope, cwd)
	}
	return LocalSkillsDir(scope, cwd)
}

// McpOriginalPath is the file that owns a server.
// A registry install lives in .agents/mcp.json. A hand-added server lives in .synca/local/mcp.json.
func McpOriginalPath(scope Scope, cwd string, installed bool) string {
	if installed {
		return InstalledMcpPath(scope, cwd)
	}
	return LocalMcpPath(scope, cwd)
}

func skillIsRemote(origin string) bool { return origin == "remote" }

func mcpIsInstalled(sourceType *string) bool {
	return sourceType != nil && *sourceType == "registry"
}

// ensureInstalledGitignored makes project .agents ignored via .git/info/exclude.
// A tracked .gitignore is left alone. A directory that is not a git repo is skipped.
func ensureInstalledGitignored(scope Scope, cwd string) {
	if scope != ScopeProject {
		return
	}
	root := ProjectRoot(cwd)
	if root == "" {
		return
	}
	if exec.Command("git", "-C", root, "check-ignore", "-q", ".agents").Run() == nil {
		return
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	existing, _ := os.ReadFile(path)
	if strings.Contains(string(existing), ".agents/") || strings.Contains(string(existing), ".agents\n") {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n# synca: installed skills and MCP stay out of git\n.agents/\n")
}

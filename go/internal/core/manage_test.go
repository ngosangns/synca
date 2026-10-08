package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func logHas(log []string, sub string) bool {
	for _, l := range log {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestSkillInstallAndUnlink(t *testing.T) {
	home := realTempHome(t)
	src := filepath.Join(home, "src-skill")
	writeSkill(t, src, "fresh-skill", "# fresh")
	skillDir := filepath.Join(src, "fresh-skill")

	plan, log, err := InstallSkill(ScopeUser, home, skillDir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) == 0 || len(log) == 0 {
		t.Fatalf("empty plan/log: %+v %v", plan.Actions, log)
	}
	canon := filepath.Join(home, ".agents/skills/fresh-skill/SKILL.md")
	if st, err := os.Stat(canon); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("canonical missing: %v", err)
	}
	grok := filepath.Join(home, ".grok/skills/fresh-skill")
	if !isSymlink(grok) {
		t.Fatal("expected grok symlink")
	}

	// unlink only
	_, log2, err := RemoveSkill(ScopeUser, home, "fresh-skill", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log2, "unlinked") {
		t.Errorf("log=%v", log2)
	}
	if _, err := os.Lstat(grok); err == nil {
		t.Error("grok link should be gone")
	}
	if _, err := os.Stat(canon); err != nil {
		t.Error("canonical must remain after unlink")
	}

	// purge
	_, log3, err := RemoveSkill(ScopeUser, home, "fresh-skill", nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log3, "purged") {
		t.Errorf("log=%v", log3)
	}
	if _, err := os.Stat(canon); err == nil {
		t.Error("canonical should be purged")
	}
}

func TestMcpAddAndRemove(t *testing.T) {
	home := realTempHome(t)
	cmd := "npx -y demo-mcp"
	norm, err := McpFromCLI("stdio", &cmd, nil, bp(true))
	if err != nil {
		t.Fatal(err)
	}
	_, log, err := AddMcp(ScopeUser, home, "demo-mcp", norm, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) == 0 {
		t.Error("empty log")
	}
	hub := filepath.Join(home, ".agents/mcp.json")
	servers, _ := readJSON(t, hub)["mcpServers"].(map[string]any)
	if _, ok := servers["demo-mcp"]; !ok {
		t.Fatalf("demo-mcp missing from hub: %v", servers)
	}

	_, log2, err := RemoveMcp(ScopeUser, home, "demo-mcp", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log2, "hub remove") {
		t.Errorf("log=%v", log2)
	}
	servers2, _ := readJSON(t, hub)["mcpServers"].(map[string]any)
	if _, ok := servers2["demo-mcp"]; ok {
		t.Error("demo-mcp still in hub")
	}
}

func TestMcpFromCLIMapsRemoteAndLocalAliases(t *testing.T) {
	r, err := McpFromCLI("remote", nil, sp("https://x/mcp"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Transport != "http" {
		t.Errorf("remote → %q", r.Transport)
	}
	l, err := McpFromCLI("local", sp("uvx tool serve"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Transport != "stdio" {
		t.Errorf("local → %q", l.Transport)
	}
	if len(l.Args) != 2 || l.Args[0] != "tool" || l.Args[1] != "serve" {
		t.Errorf("args %v", l.Args)
	}
	if _, err := McpFromCLI("bogus", nil, nil, nil); err == nil {
		t.Error("expected error for bogus transport")
	}
}

func TestMcpFromCLIURL(t *testing.T) {
	n, err := McpFromCLI("sse", nil, sp("https://example.com/mcp"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Transport != "http" {
		t.Errorf("transport %q", n.Transport)
	}
	if n.URL == nil || *n.URL != "https://example.com/mcp" {
		t.Errorf("url %v", n.URL)
	}
}

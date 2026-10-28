package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An agent config that does not parse must survive a write attempt untouched.
func TestMcpWriteRefusesUnparseableConfig(t *testing.T) {
	norm := &McpNormalized{Transport: "stdio", Command: []string{"uvx"}, Env: map[string]string{}}
	cases := []struct {
		name  string
		agent AgentKind
		file  string
		body  string
	}{
		{"json with comment", AgentCursor, "mcp.json", "{ // keep\n \"mcpServers\": {\"keep\": {\"command\": \"x\"}} }"},
		{"json truncated", AgentClaude, "claude.json", `{"mcpServers": {"keep": `},
		{"json servers wrong type", AgentCursor, "mcp2.json", `{"mcpServers": ["keep"]}`},
		{"opencode wrong type", AgentOpenCode, "opencode.json", `{"mcp": "keep"}`},
		{"toml broken", AgentGrok, "config.toml", "[mcp_servers.keep\ncommand = \"x\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.file)
			mustWrite(t, path, c.body)
			if err := WriteMcpToAgent(path, c.agent, "svc", norm); err == nil {
				t.Fatal("expected an error")
			}
			got, _ := os.ReadFile(path)
			if string(got) != c.body {
				t.Errorf("file was modified:\n%s", got)
			}
		})
	}
}

func TestMcpHubUpsertRefusesUnparseableHub(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "mcp.json")
	const body = `{"mcpServers": {"keep": {"command": "x"},}}`
	mustWrite(t, hub, body)
	norm := &McpNormalized{Transport: "stdio", Command: []string{"uvx"}}
	if err := UpsertMcpJSONHub(hub, "svc", norm); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := os.ReadFile(hub); string(got) != body {
		t.Errorf("hub was modified:\n%s", got)
	}
}

// An empty config file is a valid starting point.
func TestMcpWriteAcceptsEmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	mustWrite(t, path, "  \n")
	norm := &McpNormalized{Transport: "stdio", Command: []string{"uvx"}}
	if err := WriteMcpToAgent(path, AgentCursor, "svc", norm); err != nil {
		t.Fatal(err)
	}
	if _, ok := readJSON(t, path)["mcpServers"].(map[string]any)["svc"]; !ok {
		t.Error("svc not written")
	}
}

// A symlinked config (dotfile manager) stays a symlink after a write.
func TestMcpWriteKeepsSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "mcp.json")
	mustWrite(t, real, `{"mcpServers":{}}`)
	link := filepath.Join(dir, "mcp.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	norm := &McpNormalized{Transport: "stdio", Command: []string{"uvx"}}
	if err := WriteMcpToAgent(link, AgentCursor, "svc", norm); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(link) {
		t.Error("symlink was replaced by a file")
	}
	if _, ok := readJSON(t, real)["mcpServers"].(map[string]any)["svc"]; !ok {
		t.Error("svc not written to the real file")
	}
}

// A copy that fails midway must leave the old directory in place.
func TestReplaceDirWithCopyKeepsOldOnFailure(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(dst, "SKILL.md"), "old")
	src := filepath.Join(root, "src")
	mustWrite(t, filepath.Join(src, "SKILL.md"), "new")
	secret := filepath.Join(src, "locked", "file")
	mustWrite(t, secret, "x")
	if err := os.Chmod(filepath.Dir(secret), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(secret), 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	if err := replaceDirWithCopy(src, dst); err == nil {
		t.Fatal("expected the copy to fail")
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "SKILL.md")); string(got) != "old" {
		t.Errorf("old directory changed: %q", got)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".synca-") {
			t.Errorf("staging directory left behind: %s", e.Name())
		}
	}
}

func TestReplaceDirWithCopySwapsContent(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(dst, "old.txt"), "old")
	src := filepath.Join(root, "src")
	mustWrite(t, filepath.Join(src, "SKILL.md"), "new")
	if err := replaceDirWithCopy(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "old.txt")); err == nil {
		t.Error("stale file survived the swap")
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "SKILL.md")); string(got) != "new" {
		t.Errorf("content = %q", got)
	}
}

// Replacing a real directory with a link must not lose the directory when the
// link cannot be created.
func TestForceSymlinkRestoresDirectoryOnFailure(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "agent", "skill")
	mustWrite(t, filepath.Join(link, "SKILL.md"), "mine")
	target := filepath.Join(root, "canon", "skill")
	mustWrite(t, filepath.Join(target, "SKILL.md"), "canon")
	// Make the parent read-only after the directory exists: rename out of it fails,
	// and the original must be intact.
	parent := filepath.Dir(link)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	var log []string
	if err := ForceSymlink(link, target, "grok", &log); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := os.ReadFile(filepath.Join(link, "SKILL.md")); string(got) != "mine" {
		t.Errorf("directory lost: %q", got)
	}
}

// One bad action must not stop the others. The failing skill gets no link.
func TestApplyPlanContinuesAfterFailure(t *testing.T) {
	home := realTempHome(t)
	agents := filepath.Join(home, ".agents", "skills")
	grok := filepath.Join(home, ".grok", "skills")
	writeSkill(t, grok, "good", "g")
	writeSkill(t, grok, "bad", "b")
	mustMkdir(t, agents)

	plan, err := PlanSyncSkills(ScopeUser, home, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Block the canonical copy of "bad" with a file that cannot be replaced.
	if err := os.Chmod(agents, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agents, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	log, err := ApplyPlan(plan, home, ScopeUser, NewDecisions(ConflictSkip))
	if err == nil {
		t.Fatal("expected an aggregate error")
	}
	_ = log
	if !strings.Contains(err.Error(), "good") || !strings.Contains(err.Error(), "bad") {
		t.Errorf("both skills should be attempted and reported: %v", err)
	}
	// The agent's own directory is still a real directory with its content.
	for _, name := range []string{"good", "bad"} {
		if _, rerr := os.ReadFile(filepath.Join(grok, name, "SKILL.md")); rerr != nil {
			t.Errorf("%s lost its content: %v", name, rerr)
		}
	}
}

func TestScanSkillsIgnoresInterruptedWriteLeftovers(t *testing.T) {
	home := realTempHome(t)
	agents := filepath.Join(home, ".agents", "skills")
	writeSkill(t, agents, "real", "r")
	writeSkill(t, agents, ".synca-stage-123", "x")
	writeSkill(t, agents, "real.synca-old-9", "x")
	for _, s := range ScanSkills(ScopeUser, home) {
		if s.Key != "real" {
			t.Errorf("leftover listed as skill: %s", s.Key)
		}
	}
}

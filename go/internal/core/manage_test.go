package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
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
	canon := filepath.Join(home, ".synca/local/skills/fresh-skill/SKILL.md")
	if st, err := os.Lstat(canon); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("canonical missing: %v", err)
	}
	grok := filepath.Join(home, ".grok/skills/fresh-skill")
	installed := filepath.Join(home, ".agents/skills/fresh-skill")
	if !isSymlink(grok) {
		t.Fatal("expected a grok symlink to the local skill")
	}
	if _, err := os.Lstat(installed); !os.IsNotExist(err) {
		t.Fatalf("local skill must not be copied into .agents: %v", err)
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
	if _, err := os.Lstat(installed); !os.IsNotExist(err) {
		t.Error("installed dir should stay empty for a local skill")
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
	hub := filepath.Join(home, ".synca/local/mcp.json")
	cursorHub := filepath.Join(home, ".cursor/mcp.json")
	for _, path := range []string{hub, cursorHub} {
		servers, _ := readJSON(t, path)["mcpServers"].(map[string]any)
		if _, ok := servers["demo-mcp"]; !ok {
			t.Fatalf("demo-mcp missing from %s: %v", path, servers)
		}
	}
	installedMcp := filepath.Join(home, ".agents/mcp.json")
	if st, err := os.Lstat(installedMcp); err == nil {
		if servers, _ := readJSON(t, installedMcp)["mcpServers"].(map[string]any); servers["demo-mcp"] != nil {
			t.Fatalf("a hand-added server must not be stored in .agents/mcp.json (%v)", st.Mode())
		}
	}

	_, log2, err := RemoveMcp(ScopeUser, home, "demo-mcp", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log2, "hub remove") {
		t.Errorf("log=%v", log2)
	}
	servers2, _ := readJSON(t, hub)["mcpServers"].(map[string]any)
	if _, ok := servers2["demo-mcp"]; ok {
		t.Errorf("demo-mcp still in %s", hub)
	}
}

func TestMcpRemoveKeepHub(t *testing.T) {
	home := realTempHome(t)
	cmd := "npx -y demo-mcp"
	norm, err := McpFromCLI("stdio", &cmd, nil, bp(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddMcp(ScopeUser, home, "demo-mcp", norm, nil, false); err != nil {
		t.Fatal(err)
	}

	if _, err := PlanRemoveMcp(ScopeUser, home, "demo-mcp", nil, true); err == nil {
		t.Fatal("--keep-hub without --agents must be refused")
	}

	_, log, err := RemoveMcp(ScopeUser, home, "demo-mcp", []AgentKind{AgentCursor}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log, "removed mcp demo-mcp") {
		t.Errorf("log=%v", log)
	}
	hubServers, _ := readJSON(t, filepath.Join(home, ".synca/local/mcp.json"))["mcpServers"].(map[string]any)
	if _, ok := hubServers["demo-mcp"]; !ok {
		t.Fatal("hub must keep the server with --keep-hub")
	}
	cursorServers, _ := readJSON(t, filepath.Join(home, ".cursor/mcp.json"))["mcpServers"].(map[string]any)
	if _, ok := cursorServers["demo-mcp"]; ok {
		t.Fatal("cursor must lose the server")
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
func TestParseSource(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		in                      string
		url, source, typ, skill string
		local                   bool
		bad                     bool
	}{
		{in: "vercel-labs/agent-skills", url: "https://github.com/vercel-labs/agent-skills.git", source: "vercel-labs/agent-skills", typ: "github"},
		{in: "vercel-labs/agent-skills@find-skills", url: "https://github.com/vercel-labs/agent-skills.git", source: "vercel-labs/agent-skills", typ: "github", skill: "find-skills"},
		{in: "https://skills.sh/vercel-labs/agent-skills/find-skills", url: "https://github.com/vercel-labs/agent-skills.git", source: "vercel-labs/agent-skills", typ: "github", skill: "find-skills"},
		{in: "https://skills.sh/vercel-labs/agent-skills", url: "https://github.com/vercel-labs/agent-skills.git", source: "vercel-labs/agent-skills", typ: "github"},
		{in: "https://github.com/vercel-labs/agent-skills.git", url: "https://github.com/vercel-labs/agent-skills.git", source: "vercel-labs/agent-skills", typ: "github"},
		{in: "git@github.com:o/r.git", url: "https://github.com/o/r.git", source: "o/r", typ: "github"},
		{in: "https://gitlab.com/o/r.git", url: "https://gitlab.com/o/r.git", source: "https://gitlab.com/o/r.git", typ: "git"},
		{in: dir, local: true},
		{in: "https://skills.sh/p/some-pack", bad: true},
		{in: "--upload-pack=evil", bad: true},
		{in: "  ", bad: true},
	}
	for _, tc := range cases {
		got, err := ParseSource(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("ParseSource(%q) accepted", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSource(%q): %v", tc.in, err)
			continue
		}
		if got.Local != tc.local || got.URL != tc.url || got.Source != tc.source || got.Type != tc.typ || got.Skill != tc.skill {
			t.Errorf("ParseSource(%q) = %+v", tc.in, got)
		}
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

func TestClearProviderKeepsHubAndOtherConfig(t *testing.T) {
	home := realTempHome(t)
	src := filepath.Join(home, "src-skill")
	writeSkill(t, src, "fresh-skill", "# fresh")
	if _, _, err := InstallSkill(ScopeUser, home, filepath.Join(src, "fresh-skill"), nil, false); err != nil {
		t.Fatal(err)
	}
	cmd := "npx -y demo-mcp"
	norm, err := McpFromCLI("stdio", &cmd, nil, bp(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddMcp(ScopeUser, home, "demo-mcp", norm, nil, false); err != nil {
		t.Fatal(err)
	}

	grokToml := filepath.Join(home, ".grok/config.toml")
	text, err := os.ReadFile(grokToml)
	if err != nil {
		t.Fatal(err)
	}
	var grok map[string]any
	if _, err := toml.Decode(string(text), &grok); err != nil {
		t.Fatal(err)
	}
	grok["theme"] = "dark"
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(grok); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokToml, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	cursorPath := filepath.Join(home, ".cursor/mcp.json")
	cursor := readJSON(t, cursorPath)
	cursor["editorFont"] = "menlo"
	raw, err := json.MarshalIndent(cursor, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ClearProvider(ScopeUser, home, "agents", false); err == nil {
		t.Fatal("canonical hub must be refused")
	}
	if _, err := ClearProvider(ScopeUser, home, "nope", false); err == nil {
		t.Fatal("unknown provider must be refused")
	}

	preview, err := ClearProvider(ScopeUser, home, "grok", true)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(preview, "would remove") {
		t.Fatalf("dry-run log=%v", preview)
	}
	if _, err := os.Lstat(filepath.Join(home, ".grok/skills")); err != nil {
		t.Fatal("dry-run removed the skills root")
	}

	log, err := ClearProvider(ScopeUser, home, "grok", false)
	if err != nil {
		t.Fatal(err)
	}
	if !logHas(log, "removed skills root") || !logHas(log, "mcp server") {
		t.Fatalf("log=%v", log)
	}
	if _, err := os.Lstat(filepath.Join(home, ".grok/skills")); err == nil {
		t.Fatal("grok skills root should be gone")
	}
	canon := filepath.Join(home, ".synca/local/skills/fresh-skill/SKILL.md")
	if _, err := os.Stat(canon); err != nil {
		t.Fatal("canonical skill must remain")
	}
	kept := map[string]any{}
	rest, err := os.ReadFile(grokToml)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.Decode(string(rest), &kept); err != nil {
		t.Fatal(err)
	}
	if kept["theme"] != "dark" {
		t.Fatalf("grok theme lost: %v", kept)
	}
	if servers, ok := kept["mcp_servers"].(map[string]any); ok && len(servers) > 0 {
		t.Fatalf("grok mcp servers remain: %v", servers)
	}
	hubServers, _ := readJSON(t, filepath.Join(home, ".synca/local/mcp.json"))["mcpServers"].(map[string]any)
	if _, ok := hubServers["demo-mcp"]; !ok {
		t.Fatal("hub mcp entry must remain")
	}
	if _, err := ClearProvider(ScopeUser, home, "cursor", false); err != nil {
		t.Fatal(err)
	}
	after := readJSON(t, cursorPath)
	if after["editorFont"] != "menlo" {
		t.Fatalf("cursor setting lost: %v", after)
	}
	if servers, ok := after["mcpServers"].(map[string]any); ok && len(servers) > 0 {
		t.Fatalf("cursor mcp servers remain: %v", servers)
	}
}

func TestProjectAgentsDirIsGitignored(t *testing.T) {
	home := realTempHome(t)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", proj).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	src := filepath.Join(home, "src")
	writeSkill(t, src, "hand", "# hand")
	if _, _, err := InstallSkill(ScopeProject, proj, filepath.Join(src, "hand"), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", proj, "check-ignore", "-q", ".agents").Run(); err != nil {
		excludePath, _ := exec.Command("git", "-C", proj, "rev-parse", "--git-path", "info/exclude").Output()
		body, _ := os.ReadFile(strings.TrimSpace(string(excludePath)))
		t.Fatalf("project .agents is not gitignored (%s):\n%s", strings.TrimSpace(string(excludePath)), body)
	}
	if err := exec.Command("git", "-C", proj, "check-ignore", "-q", ".synca/local").Run(); err == nil {
		t.Fatal(".synca/local must stay committable")
	}
}

func TestLocalStoreIsTheOriginal(t *testing.T) {
	home := realTempHome(t)
	src := filepath.Join(home, "src-skill")
	writeSkill(t, src, "fresh-skill", "# fresh")
	if _, _, err := InstallSkill(ScopeUser, home, filepath.Join(src, "fresh-skill"), nil, false); err != nil {
		t.Fatal(err)
	}
	storeSkill := filepath.Join(home, ".synca", "local", "skills", "fresh-skill")
	body, err := os.ReadFile(filepath.Join(storeSkill, "SKILL.md"))
	if err != nil || !strings.Contains(string(body), "# fresh") {
		t.Fatalf("store skill: %v %q", err, body)
	}
	if st, err := os.Lstat(storeSkill); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("store must be a real directory: %v %#v", err, st)
	}
	storeReal, err := filepath.EvalSymlinks(storeSkill)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", "fresh-skill")); !os.IsNotExist(err) {
		t.Fatalf("local skill copied into .agents: %v", err)
	}
	for _, link := range []string{
		filepath.Join(home, ".grok", "skills", "fresh-skill"),
	} {
		got, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if got != storeReal {
			t.Fatalf("%s resolved to %s, want %s", link, got, storeReal)
		}
	}

	cmd := "npx -y demo-mcp"
	norm, err := McpFromCLI("stdio", &cmd, nil, bp(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddMcp(ScopeUser, home, "demo-mcp", norm, nil, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".synca", "local", "mcp.json"),
		filepath.Join(home, ".cursor", "mcp.json"),
	} {
		servers, _ := readJSON(t, path)["mcpServers"].(map[string]any)
		if _, ok := servers["demo-mcp"]; !ok {
			t.Fatalf("demo-mcp missing from %s", path)
		}
	}

	proj := filepath.Join(home, "proj")
	mustMkdir(t, filepath.Join(proj, ".git"))
	if _, _, err := InstallSkill(ScopeProject, proj, filepath.Join(src, "fresh-skill"), nil, false); err != nil {
		t.Fatal(err)
	}
	projStore := filepath.Join(proj, ".synca", "local", "skills", "fresh-skill")
	if _, err := os.ReadFile(filepath.Join(projStore, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	projReal, err := filepath.EvalSymlinks(projStore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(proj, ".agents", "skills", "fresh-skill")); !os.IsNotExist(err) {
		t.Fatalf("project local skill copied into .agents: %v", err)
	}
	projGrok, err := filepath.EvalSymlinks(filepath.Join(proj, ".grok", "skills", "fresh-skill"))
	if err != nil || projGrok != projReal {
		t.Fatalf("project grok link -> %s (%v), want %s", projGrok, err, projReal)
	}
	if _, _, err := AddMcp(ScopeProject, proj, "demo-mcp", norm, nil, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(proj, ".synca", "local", "mcp.json"),
		filepath.Join(proj, ".mcp.json"),
	} {
		servers, _ := readJSON(t, path)["mcpServers"].(map[string]any)
		if _, ok := servers["demo-mcp"]; !ok {
			t.Fatalf("demo-mcp missing from %s", path)
		}
	}

	home = realTempHome(t)
	mirror := filepath.Join(home, ".agents", "skills", "kept")
	mustWrite(t, filepath.Join(mirror, "SKILL.md"), "---\nname: kept\n---\n# kept\n")
	mustWrite(t, filepath.Join(mirror, "scripts", "run.sh"), "echo kept\n")
	plan, err := PlanSyncSkills(ScopeUser, home, nil, "kept")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(plan, home, ScopeUser, NewDecisions(ConflictSkip)); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(home, ".synca", "local", "skills", "kept")
	extra, err := os.ReadFile(filepath.Join(kept, "scripts", "run.sh"))
	if err != nil || string(extra) != "echo kept\n" {
		t.Fatalf("store extra file: %v %q", err, extra)
	}
	if _, err := os.Lstat(mirror); !os.IsNotExist(err) {
		t.Fatalf("local skill should leave .agents after sync: %v", err)
	}
	if st, err := os.Lstat(kept); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatal("store became a symlink")
	}

	writeSkill(t, filepath.Join(home, "src-skill"), "fetched", "# fetched")
	origin := &LockEntry{Source: "o/r", SourceType: "github", FolderHash: "abc"}
	installedPlan, err := PlanInstallSkill(ScopeUser, home, filepath.Join(home, "src-skill", "fetched"), nil, origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyManagePlan(installedPlan, nil); err != nil {
		t.Fatal(err)
	}
	fetched := filepath.Join(home, ".agents", "skills", "fetched")
	if st, err := os.Lstat(fetched); err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		t.Fatalf("installed skill must be a real directory in .agents: %v %#v", err, st)
	}
	if _, err := os.Lstat(filepath.Join(home, ".synca", "local", "skills", "fetched")); !os.IsNotExist(err) {
		t.Fatalf("installed skill must not land in .synca/local: %v", err)
	}
	got, err := filepath.EvalSymlinks(filepath.Join(home, ".grok", "skills", "fetched"))
	want, werr := filepath.EvalSymlinks(fetched)
	if err != nil || werr != nil || got != want {
		t.Fatalf("installed grok link -> %s (%v), want %s (%v)", got, err, want, werr)
	}
}

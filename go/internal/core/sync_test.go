package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, name, "SKILL.md"), "---\nname: "+name+"\n---\n"+body+"\n")
}

func writeRawSkill(t *testing.T, dir, folder, frontmatter, body string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, folder, "SKILL.md"), "---\n"+frontmatter+"\n---\n"+body+"\n")
}

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func isSymlink(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

func realTempHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

func TestSkillSyncDryRunSymlinkPlan(t *testing.T) {
	home := realTempHome(t)
	agents := filepath.Join(home, ".agents/skills")
	mustMkdir(t, agents)
	mustMkdir(t, filepath.Join(home, ".grok/skills"))
	writeSkill(t, agents, "demo-skill", "# demo")

	plan, err := PlanSyncSkills(ScopeUser, home, nil, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range plan.Actions {
		if a.Kind == "symlink_skill" && strings.HasPrefix(a.Link, filepath.Join(home, ".grok/skills")) &&
			strings.HasSuffix(a.Link, "demo-skill") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected symlink_skill for grok, got %+v", plan.Actions)
	}
	// dry-run planning must not touch the filesystem
	if _, err := os.Lstat(filepath.Join(home, ".grok/skills/demo-skill")); err == nil {
		t.Error("planning created the link")
	}
}

func clashSetup(t *testing.T) (home, agents, grok string) {
	home = realTempHome(t)
	agents = filepath.Join(home, ".agents/skills")
	grok = filepath.Join(home, ".grok/skills")
	mustMkdir(t, agents)
	mustMkdir(t, grok)
	writeSkill(t, agents, "clash", "# version A")
	writeSkill(t, grok, "clash", "# version B different")
	return
}

func TestSkillConflictDetectedWhenHashesDiffer(t *testing.T) {
	home, _, _ := clashSetup(t)
	var clash *SkillEntry
	skills := ScanSkills(ScopeUser, home)
	for i := range skills {
		if skills[i].Key == "clash" {
			clash = &skills[i]
		}
	}
	if clash == nil {
		t.Fatal("clash skill not scanned")
	}
	if !clash.Mismatch {
		t.Error("expected mismatch")
	}
	plan, err := PlanSyncSkills(ScopeUser, home, nil, "clash")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range plan.Actions {
		if a.Kind == "conflict_skill" && a.SkillKey == "clash" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected conflict_skill, got %+v", plan.Actions)
	}
}

func TestSkillConflictKeepSourceOverwrites(t *testing.T) {
	home, agents, grok := clashSetup(t)
	plan, err := PlanSyncSkills(ScopeUser, home, nil, "clash")
	if err != nil {
		t.Fatal(err)
	}
	log, err := ApplyPlan(plan, home, ScopeUser, NewDecisions(ConflictKeepSource))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "conflict skill") {
		t.Errorf("log=%v", log)
	}
	if !isSymlink(filepath.Join(grok, "clash")) {
		t.Errorf("expected symlink at %s", filepath.Join(grok, "clash"))
	}
	body, err := os.ReadFile(filepath.Join(agents, "clash/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "version A") {
		t.Errorf("canonical body: %q", body)
	}
}

func TestMcpHubMergePreservesTargetEnvSecrets(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "mcp.json")
	mustWrite(t, hub, `{"mcpServers":{"svc":{"type":"stdio","command":"old","env":{"SECRET":"keep-me","SHARED":"old"}}}}`)

	norm := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"new"},
		EnvKeys:   []string{"SHARED"},
		Env:       map[string]string{"SHARED": "new"},
	}
	// source lacks SECRET
	if err := UpsertMcpJSONHub(hub, "svc", norm); err != nil {
		t.Fatal(err)
	}
	svc := readJSON(t, hub)["mcpServers"].(map[string]any)["svc"].(map[string]any)
	env := svc["env"].(map[string]any)
	if env["SECRET"] != "keep-me" {
		t.Errorf("secret must be preserved: %v", env)
	}
	if svc["command"] != "new" {
		t.Errorf("command = %v", svc["command"])
	}
}

// Source env values win; keys only the target has (secrets) are kept.
func TestMcpHubMergeSourceEnvWins(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "mcp.json")
	mustWrite(t, hub, `{"mcpServers":{"svc":{"type":"stdio","command":"old","env":{"SECRET":"keep-me","SHARED":"old"}}}}`)

	norm := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"new"},
		EnvKeys:   []string{"SHARED"},
		Env:       map[string]string{"SHARED": "new"},
	}
	if err := UpsertMcpJSONHub(hub, "svc", norm); err != nil {
		t.Fatal(err)
	}
	env := readJSON(t, hub)["mcpServers"].(map[string]any)["svc"].(map[string]any)["env"].(map[string]any)
	if env["SHARED"] != "new" {
		t.Errorf("shared key from source must win when present: %v", env)
	}

	// source key is inserted when the target lacks it
	norm.Env["NEWKEY"] = "x"
	if err := UpsertMcpJSONHub(hub, "svc", norm); err != nil {
		t.Fatal(err)
	}
	env2 := readJSON(t, hub)["mcpServers"].(map[string]any)["svc"].(map[string]any)["env"].(map[string]any)
	if env2["SECRET"] != "keep-me" || env2["NEWKEY"] != "x" {
		t.Errorf("env after second upsert: %v", env2)
	}
}

func TestMcpOpencodeWriterUsesLocalRemoteSchemaAndKeepsEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	mustWrite(t, path, `{"mcp":{"svc":{"type":"local","command":["old"],"environment":{"SECRET":"keep"}}}}`)
	stdio := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"uvx"},
		Args:      []string{"serve"},
		Enabled:   bp(true),
		EnvKeys:   []string{},
		Env:       map[string]string{},
	}
	if err := WriteMcpToAgent(path, AgentOpenCode, "svc", stdio); err != nil {
		t.Fatal(err)
	}
	svc := readJSON(t, path)["mcp"].(map[string]any)["svc"].(map[string]any)
	if svc["type"] != "local" {
		t.Errorf("type = %v", svc["type"])
	}
	if !reflect.DeepEqual(svc["command"], []any{"uvx", "serve"}) {
		t.Errorf("command = %v", svc["command"])
	}
	if svc["environment"].(map[string]any)["SECRET"] != "keep" {
		t.Errorf("environment = %v", svc["environment"])
	}

	httpN := &McpNormalized{
		Transport: "http",
		URL:       sp("https://x/mcp"),
		Enabled:   bp(true),
		EnvKeys:   []string{},
		Env:       map[string]string{},
	}
	if err := WriteMcpToAgent(path, AgentOpenCode, "web", httpN); err != nil {
		t.Fatal(err)
	}
	web := readJSON(t, path)["mcp"].(map[string]any)["web"].(map[string]any)
	if web["type"] != "remote" || web["url"] != "https://x/mcp" {
		t.Errorf("web = %v", web)
	}
}

func TestMcpWritersKeepArgsWhenCommandIsSingleToken(t *testing.T) {
	tmp := t.TempDir()
	norm := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"uvx"},
		Args:      []string{"--from", "pkg", "serve"},
		Enabled:   bp(true),
		EnvKeys:   []string{},
		Env:       map[string]string{},
	}
	jsonPath := filepath.Join(tmp, "mcp.json")
	if err := WriteMcpToAgent(jsonPath, AgentPi, "svc", norm); err != nil {
		t.Fatal(err)
	}
	svc := readJSON(t, jsonPath)["mcpServers"].(map[string]any)["svc"].(map[string]any)
	if !reflect.DeepEqual(svc["args"], []any{"--from", "pkg", "serve"}) {
		t.Errorf("json args = %v", svc["args"])
	}

	tomlPath := filepath.Join(tmp, "config.toml")
	if err := WriteMcpToAgent(tomlPath, AgentGrok, "svc", norm); err != nil {
		t.Fatal(err)
	}
	servers, err := ReadMcpServers(AgentGrok, tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := servers["svc"].Args; len(got) != 3 {
		t.Errorf("toml args = %v", got)
	}
}

func TestSyncPlanNeverCrossesUserAndProjectScopes(t *testing.T) {
	home := realTempHome(t)
	agents := filepath.Join(home, ".agents/skills")
	mustMkdir(t, agents)
	writeSkill(t, agents, "user-only", "# user")

	proj := filepath.Join(home, "proj")
	mustMkdir(t, filepath.Join(proj, ".git"))
	writeSkill(t, filepath.Join(proj, ".agents/skills"), "proj-only", "# project")

	paths := func(a SyncAction) []string {
		var out []string
		switch a.Kind {
		case "ensure_canonical_copy", "collapse_skill_alias":
			out = []string{a.From, a.To}
		case "symlink_skill":
			out = []string{a.Link, a.Target}
		case "skip_same", "repair_skill_frontmatter":
			out = []string{a.Path}
		case "conflict_skill":
			out = a.Paths
		}
		return out
	}
	hasKey := func(p *SyncPlan, key string) bool {
		for _, a := range p.Actions {
			switch a.Kind {
			case "ensure_canonical_copy", "symlink_skill", "skip_same", "conflict_skill":
				if a.SkillKey == key {
					return true
				}
			}
		}
		return false
	}

	userPlan, err := PlanSyncSkills(ScopeUser, proj, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if userPlan.Scope != "user" {
		t.Errorf("scope = %q", userPlan.Scope)
	}
	for _, a := range userPlan.Actions {
		for _, p := range paths(a) {
			for _, leak := range []string{".agents", ".grok", ".kiro"} {
				if strings.HasPrefix(p, filepath.Join(proj, leak)) {
					t.Errorf("user-scope action leaked into project path %s: %+v", p, a)
				}
			}
		}
	}
	if hasKey(userPlan, "proj-only") {
		t.Errorf("user plan must not include proj-only skill: %+v", userPlan.Actions)
	}

	projPlan, err := PlanSyncSkills(ScopeProject, proj, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if projPlan.Scope != "project" {
		t.Errorf("scope = %q", projPlan.Scope)
	}
	for _, a := range projPlan.Actions {
		for _, p := range paths(a) {
			if !strings.HasPrefix(p, proj) {
				t.Errorf("project-scope action escaped project root %s: %+v", p, a)
			}
		}
	}
	if hasKey(projPlan, "user-only") {
		t.Errorf("project plan must not include user-only skill: %+v", projPlan.Actions)
	}

	n := len(userPlan.Actions)
	mixed := MergePlans(userPlan, projPlan)
	if mixed.Scope != "user" || len(mixed.Actions) != n {
		t.Errorf("MergePlans must refuse to combine scopes: scope=%s actions=%d want %d", mixed.Scope, len(mixed.Actions), n)
	}
}

func TestPlanSyncMcpRecordsSingleScope(t *testing.T) {
	home := realTempHome(t)
	mustWrite(t, filepath.Join(home, ".agents/mcp.json"), `{"mcpServers":{"demo":{"type":"stdio","command":"echo"}}}`)
	plan, err := PlanSyncMcp(ScopeUser, home, nil, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Scope != "user" {
		t.Errorf("scope = %q", plan.Scope)
	}
	for _, a := range plan.Actions {
		if a.Kind == "ensure_mcp_hub" && !strings.HasSuffix(a.Hub, ".agents/mcp.json") {
			t.Errorf("hub = %s", a.Hub)
		}
		if a.Kind == "write_mcp_server" && strings.Contains(a.Path, "/proj/") {
			t.Errorf("user mcp write leaked: %s", a.Path)
		}
	}
}

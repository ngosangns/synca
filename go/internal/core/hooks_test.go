package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hooksHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func saveHub(t *testing.T, hooks map[string]Hook) {
	t.Helper()
	if err := SaveHooksFile(&HooksFile{Version: 1, Hooks: hooks}); err != nil {
		t.Fatal(err)
	}
}

func trustAll(t *testing.T) {
	t.Helper()
	hub, err := LoadHooksFile()
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := LoadTrust()
	for id, h := range hub.Hooks {
		if err := tr.Approve("user", id, HookHash(id, h)); err != nil {
			t.Fatal(err)
		}
	}
}

func syncHooks(t *testing.T, filter []AgentKind, policy ConflictPolicy) (*HooksPlan, []string) {
	t.Helper()
	plan, err := PlanSyncHooks(filter, policy)
	if err != nil {
		t.Fatal(err)
	}
	log, err := ApplyHooksPlan(plan)
	if err != nil {
		t.Fatalf("apply: %v\n%v", err, log)
	}
	return plan, log
}

func kinds(p *HooksPlan, agent string) map[string]int {
	out := map[string]int{}
	for _, a := range p.Actions {
		if a.Agent == agent || agent == "" {
			out[a.Kind]++
		}
	}
	return out
}

func shellGuard() Hook {
	return Hook{Event: "PreToolUse", Tools: []string{"shell"}, Handler: HookHandler{Command: "{{hook_dir}}/check.sh", TimeoutSec: 10}, OnFailure: "block"}
}

func TestParseHooksFileValidation(t *testing.T) {
	good := `{"version":1,"hooks":{"a-b":{"event":"Stop","handler":{"command":"echo hi"}}}}`
	if _, err := ParseHooksFile([]byte(good)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown field":   `{"version":1,"hooks":{"a":{"event":"Stop","handler":{"command":"x"},"bogus":1}}}`,
		"bad event":       `{"version":1,"hooks":{"a":{"event":"Nope","handler":{"command":"x"}}}}`,
		"tools on Stop":   `{"version":1,"hooks":{"a":{"event":"Stop","tools":["shell"],"handler":{"command":"x"}}}}`,
		"bad tool":        `{"version":1,"hooks":{"a":{"event":"PreToolUse","tools":["nope"],"handler":{"command":"x"}}}}`,
		"empty command":   `{"version":1,"hooks":{"a":{"event":"Stop","handler":{"command":" "}}}}`,
		"bad id":          `{"version":1,"hooks":{"A_b":{"event":"Stop","handler":{"command":"x"}}}}`,
		"secret":          `{"version":1,"hooks":{"a":{"event":"Stop","handler":{"command":"curl --token=abc123 https://x"}}}}`,
		"future version":  `{"version":2,"hooks":{}}`,
		"missing version": `{"hooks":{}}`,
		"bad agent":       `{"version":1,"hooks":{"a":{"event":"Stop","agents":["vim"],"handler":{"command":"x"}}}}`,
		"bad onFailure":   `{"version":1,"hooks":{"a":{"event":"Stop","onFailure":"maybe","handler":{"command":"x"}}}}`,
		"http handler":    `{"version":1,"hooks":{"a":{"event":"Stop","handler":{"type":"http","command":"x"}}}}`,
	}
	for name, in := range cases {
		if _, err := ParseHooksFile([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestHookHashCoversScripts(t *testing.T) {
	hooksHome(t)
	h := shellGuard()
	before := HookHash("guard", h)
	writeFile(t, filepath.Join(HubHookDir("guard"), "check.sh"), "#!/bin/sh\nexit 0\n")
	withScript := HookHash("guard", h)
	if before == withScript {
		t.Fatal("adding a script must change the hash")
	}
	writeFile(t, filepath.Join(HubHookDir("guard"), "check.sh"), "#!/bin/sh\nexit 2\n")
	if HookHash("guard", h) == withScript {
		t.Fatal("editing a script must change the hash")
	}
	off := false
	h.Enabled = &off
	if HookHash("guard", h) != HookHash("guard", shellGuard()) {
		t.Fatal("enabled must not change the hash")
	}
}

const claudeSettings = `{
  "model" : "opus",
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/foreign/pre.sh"}]}
    ],
    "Notification": [
      {"hooks": [{"type": "command", "command": "/foreign/notify.sh"}]}
    ]
  },
  "theme": "dark"
}
`

func TestClaudeSharedFileLifecycle(t *testing.T) {
	home := hooksHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, settings, claudeSettings)
	saveHub(t, map[string]Hook{"guard": shellGuard(), "done": {Event: "Stop", Handler: HookHandler{Command: "echo done"}}})

	// Untrusted: reported, never written.
	plan, _ := syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if plan.Untrusted != 2 || kinds(plan, "claude")["conflict_hook"] != 2 {
		t.Fatalf("expected 2 untrusted, got %+v", plan.Actions)
	}
	if readFile(t, settings) != claudeSettings {
		t.Fatal("an untrusted hook changed the file")
	}

	trustAll(t)
	plan, _ = syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if kinds(plan, "claude")["write_hook"] != 2 {
		t.Fatalf("expected 2 writes, got %+v", plan.Actions)
	}
	got := readFile(t, settings)
	if !json.Valid([]byte(got)) {
		t.Fatalf("invalid JSON written:\n%s", got)
	}
	// Bytes outside "hooks" are preserved exactly.
	if !strings.HasPrefix(got, "{\n  \"model\" : \"opus\",\n  \"permissions\": {\"allow\": [\"Bash(ls:*)\"]},\n  \"hooks\": ") ||
		!strings.HasSuffix(got, ",\n  \"theme\": \"dark\"\n}\n") {
		t.Fatalf("non-hooks bytes changed:\n%s", got)
	}
	var parsed map[string]any
	_ = json.Unmarshal([]byte(got), &parsed)
	hooks := parsed["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 || !strings.Contains(got, "/foreign/pre.sh") || !strings.Contains(got, "/foreign/notify.sh") {
		t.Fatalf("foreign hooks lost:\n%s", got)
	}
	if !strings.Contains(got, filepath.Join(home, ".agents", "hooks", "guard")+"/check.sh") {
		t.Fatalf("{{hook_dir}} not rendered:\n%s", got)
	}
	if !strings.Contains(got, `"matcher": "Bash"`) || strings.Contains(got, `\u0026`) {
		t.Fatalf("unexpected render:\n%s", got)
	}
	var guardPlanLossy bool
	for _, a := range plan.Actions {
		if a.Hook == "guard" && len(a.Lossy) == 1 && a.Lossy[0].Code == LossyFailureModeWeakened {
			guardPlanLossy = true
		}
	}
	if !guardPlanLossy {
		t.Fatalf("onFailure=block must be reported as lossy: %+v", plan.Actions)
	}

	// Idempotent.
	plan, log := syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if k := kinds(plan, "claude"); k["skip_hook_same"] != 2 || len(k) != 1 || len(log) != 0 {
		t.Fatalf("second run must change nothing: %+v %v", plan.Actions, log)
	}
	if readFile(t, settings) != got {
		t.Fatal("second run rewrote the file")
	}

	// Hub change needs a new approval; until then the old entry stays.
	h := shellGuard()
	h.Handler.Command = "{{hook_dir}}/check2.sh"
	saveHub(t, map[string]Hook{"guard": h, "done": {Event: "Stop", Handler: HookHandler{Command: "echo done"}}})
	plan, _ = syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if plan.Untrusted != 1 || readFile(t, settings) != got {
		t.Fatalf("changed hook must be held: %+v", plan.Actions)
	}
	trustAll(t)
	plan, _ = syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if kinds(plan, "claude")["update_hook"] != 1 {
		t.Fatalf("expected update: %+v", plan.Actions)
	}
	if strings.Contains(readFile(t, settings), "check.sh") || !strings.Contains(readFile(t, settings), "check2.sh") {
		t.Fatal("update did not replace the entry")
	}

	// Removing from the hub removes only synca's entries; the created Stop event goes away.
	saveHub(t, map[string]Hook{})
	plan, _ = syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if kinds(plan, "claude")["remove_hook"] != 2 {
		t.Fatalf("expected 2 removals: %+v", plan.Actions)
	}
	after := readFile(t, settings)
	if strings.Contains(after, "check2.sh") || strings.Contains(after, "echo done") || strings.Contains(after, `"Stop"`) {
		t.Fatalf("synca entries not removed:\n%s", after)
	}
	if !strings.Contains(after, "/foreign/pre.sh") || !strings.Contains(after, "/foreign/notify.sh") || !strings.Contains(after, `"model" : "opus"`) {
		t.Fatalf("foreign content damaged:\n%s", after)
	}
	lock, _ := LoadHookLock()
	if len(lock.Files) != 0 {
		t.Fatalf("lock not cleaned: %+v", lock.Files)
	}
	// A backup exists for the first write.
	entries, _ := os.ReadDir(filepath.Join(HooksStateDir(), "backups"))
	if len(entries) == 0 {
		t.Fatal("no backup written")
	}
}

func TestSharedFileWithoutHooksKeyAndMissingFile(t *testing.T) {
	home := hooksHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, settings, "{\n\t\"model\": \"x\"\n}\n")
	saveHub(t, map[string]Hook{"done": {Event: "Stop", Handler: HookHandler{Command: "echo done"}}})
	trustAll(t)
	syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	got := readFile(t, settings)
	if !json.Valid([]byte(got)) || !strings.HasPrefix(got, "{\n\t\"model\": \"x\",\n\t\"hooks\": {") {
		t.Fatalf("insert did not follow the file's style:\n%q", got)
	}

	if err := os.Remove(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(HooksStateDir()); err != nil {
		t.Fatal(err)
	}
	trustAll(t)
	syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	got = readFile(t, settings)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil || parsed["hooks"] == nil {
		t.Fatalf("created file invalid: %v\n%s", err, got)
	}
}

func TestParseErrorLeavesFileUntouched(t *testing.T) {
	home := hooksHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	jsonc := "{\n  // comment\n  \"model\": \"x\",\n}\n"
	writeFile(t, settings, jsonc)
	saveHub(t, map[string]Hook{"done": {Event: "Stop", Handler: HookHandler{Command: "echo done"}}})
	trustAll(t)
	plan, _ := syncHooks(t, []AgentKind{AgentClaude}, ConflictSkip)
	if readFile(t, settings) != jsonc {
		t.Fatal("unparseable file was modified")
	}
	found := false
	for _, a := range plan.Actions {
		if a.Kind == "conflict_hook" && strings.HasPrefix(a.Reason, "parse_error") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected parse_error: %+v", plan.Actions)
	}
}

func TestDevinKeepsForeignGroupsAndFormat(t *testing.T) {
	home := hooksHome(t)
	cfg := filepath.Join(home, ".config", "devin", "config.json")
	orig := `{
  "agent" : {
    "model" : "swe"
  },
  "hooks" : {
    "Stop": [
      {"hooks": [{"command": "/orca/stop.sh", "type": "command"}]}
    ]
  },
  "version" : 2
}
`
	writeFile(t, cfg, orig)
	saveHub(t, map[string]Hook{
		"guard":   shellGuard(),
		"compact": {Event: "PostCompact", Handler: HookHandler{Command: "echo c"}},
		"notify":  {Event: "Notification", Handler: HookHandler{Command: "echo n"}},
	})
	trustAll(t)
	plan, _ := syncHooks(t, []AgentKind{AgentDevin}, ConflictSkip)
	k := kinds(plan, "devin")
	if k["write_hook"] != 2 || k["skip_hook_unsupported"] != 1 {
		t.Fatalf("unexpected plan: %+v", plan.Actions)
	}
	got := readFile(t, cfg)
	if !strings.HasPrefix(got, "{\n  \"agent\" : {\n    \"model\" : \"swe\"\n  },\n  \"hooks\" : ") || !strings.HasSuffix(got, ",\n  \"version\" : 2\n}\n") {
		t.Fatalf("format not preserved:\n%s", got)
	}
	if !strings.Contains(got, "/orca/stop.sh") || !strings.Contains(got, `"PostCompaction"`) || !strings.Contains(got, `"matcher": "exec"`) {
		t.Fatalf("unexpected content:\n%s", got)
	}
	if strings.Contains(got, "echo n") {
		t.Fatal("unsupported event written")
	}
}

func TestGrokOwnFile(t *testing.T) {
	home := hooksHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".grok", "hooks", "synca.json")
	foreign := filepath.Join(home, ".grok", "hooks", "orca-status.json")
	writeFile(t, foreign, `{"hooks":{}}`)
	saveHub(t, map[string]Hook{
		"guard": shellGuard(),
		"done":  {Event: "Stop", Handler: HookHandler{Command: "echo done"}},
		"perm":  {Event: "PermissionRequest", Handler: HookHandler{Command: "echo p"}},
	})
	trustAll(t)
	plan, _ := syncHooks(t, []AgentKind{AgentGrok}, ConflictSkip)
	k := kinds(plan, "grok")
	if k["write_hook"] != 2 || k["skip_hook_unsupported"] != 1 {
		t.Fatalf("unexpected plan: %+v", plan.Actions)
	}
	got := readFile(t, target)
	if !json.Valid([]byte(got)) || !strings.Contains(got, `"matcher": "run_terminal_command"`) {
		t.Fatalf("bad grok file:\n%s", got)
	}
	// Event order follows the taxonomy, not map order.
	if strings.Index(got, `"PreToolUse"`) > strings.Index(got, `"Stop"`) {
		t.Fatalf("events out of order:\n%s", got)
	}
	if readFile(t, foreign) != `{"hooks":{}}` {
		t.Fatal("foreign Grok hook file touched")
	}
	plan, log := syncHooks(t, []AgentKind{AgentGrok}, ConflictSkip)
	if k := kinds(plan, "grok"); k["skip_hook_same"] != 2 || len(log) != 0 {
		t.Fatalf("not idempotent: %+v %v", plan.Actions, log)
	}

	// A hand edit is a conflict; keep-source overwrites.
	writeFile(t, target, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"evil"}]}]}}`)
	plan, _ = syncHooks(t, []AgentKind{AgentGrok}, ConflictSkip)
	if kinds(plan, "grok")["conflict_hook"] != 1 || !strings.Contains(readFile(t, target), "evil") {
		t.Fatalf("hand edit must be a conflict: %+v", plan.Actions)
	}
	syncHooks(t, []AgentKind{AgentGrok}, ConflictKeepSource)
	if strings.Contains(readFile(t, target), "evil") {
		t.Fatal("keep-source did not restore the hub version")
	}

	// Removing every hook deletes the owned file.
	saveHub(t, map[string]Hook{})
	syncHooks(t, []AgentKind{AgentGrok}, ConflictSkip)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("owned file not removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign file removed")
	}
}

func TestToolMappingAndUnmapped(t *testing.T) {
	hooksHome(t)
	var devin, claude hookAdapter
	for _, a := range hookAdapters() {
		switch a.agent {
		case AgentDevin:
			devin = a
		case AgentClaude:
			claude = a
		}
	}
	if m, _, ok := claude.matcherFor([]string{"shell", "write"}); !ok || m != "Bash|Edit|Write|MultiEdit" {
		t.Fatalf("claude matcher %q", m)
	}
	if m, _, ok := claude.matcherFor([]string{"shell", "mcp"}); !ok || m != "^(?:Bash|mcp__.*)$" {
		t.Fatalf("claude regex matcher %q", m)
	}
	if _, lossy, ok := devin.matcherFor([]string{"web"}); ok || len(lossy) != 1 {
		t.Fatal("an unmappable class set must skip the hook, never widen it")
	}
	if m, lossy, ok := devin.matcherFor([]string{"shell", "web"}); !ok || m != "exec" || len(lossy) != 1 {
		t.Fatalf("partial mapping: %q %v", m, lossy)
	}
	if _, err := devin.render("x", Hook{Event: "PreToolUse", Tools: []string{"web"}, Handler: HookHandler{Command: "x"}}); err == nil || err.Error() != "tool_unmapped" {
		t.Fatalf("expected tool_unmapped, got %v", err)
	}
	if _, err := claude.render("x", Hook{Event: "Stop", Handler: HookHandler{Command: "{{project_dir}}/x"}}); err == nil {
		t.Fatal("{{project_dir}} must be refused in user scope")
	}
}

func TestAgentFiltersAndExclusions(t *testing.T) {
	home := hooksHome(t)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), "{}\n")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o755); err != nil {
		t.Fatal(err)
	}
	off := false
	saveHub(t, map[string]Hook{
		"a": {Event: "Stop", Handler: HookHandler{Command: "echo a"}, ExcludeAgents: []string{"grok"}},
		"b": {Event: "Stop", Handler: HookHandler{Command: "echo b"}, Enabled: &off},
	})
	trustAll(t)
	plan, _ := syncHooks(t, nil, ConflictSkip)
	if kinds(plan, "grok")["write_hook"] != 0 || kinds(plan, "claude")["write_hook"] != 1 {
		t.Fatalf("exclusion/disable not honored: %+v", plan.Actions)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok", "hooks", "synca.json")); !os.IsNotExist(err) {
		t.Fatal("excluded agent got a file")
	}
}

func TestTrustStoreRoundTrip(t *testing.T) {
	hooksHome(t)
	tr, _ := LoadTrust()
	if tr.IsTrusted("user", "a", "h1") {
		t.Fatal("empty store trusts nothing")
	}
	if err := tr.Approve("user", "a", "h1"); err != nil {
		t.Fatal(err)
	}
	tr, _ = LoadTrust()
	if !tr.IsTrusted("user", "a", "h1") || tr.IsTrusted("user", "a", "h2") {
		t.Fatal("trust is keyed by hash")
	}
	st, err := os.Stat(trustPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("trust store must be 0600: %v %v", err, st)
	}
	if ok, _ := tr.Revoke("user", "a"); !ok {
		t.Fatal("revoke")
	}
}
